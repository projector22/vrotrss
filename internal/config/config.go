// Package config loads vrotrss settings from a TOML file and the environment.
package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Sample is a documented example configuration.
const Sample = `# vrotrss configuration
#
# Your FreshRSS root URL (or the full .../api/greader.php URL).
url = "https://rss.example.com"

# Your FreshRSS username.
username = "alice"

# The *API password* (FreshRSS: Profile -> API management), not your login
# password. API access must be enabled under Administration -> Authentication.
# Prefer password_cmd so the secret is not stored in this file.
# password = "api-password"
password_cmd = "pass show freshrss/api"

# Behaviour (defaults shown).
# mark_read_on_open = true   # mark an article read when you open it
# unread_only       = true   # start with only unread articles
# page_size         = 50     # articles fetched per request
# refresh_minutes   = 5      # refresh unread counts every N minutes (0 = never)
`

// Config holds all settings.
type Config struct {
	URL            string `toml:"url"`
	Username       string `toml:"username"`
	Password       string `toml:"password"`
	PasswordCmd    string `toml:"password_cmd"`
	MarkReadOnOpen bool   `toml:"mark_read_on_open"`
	UnreadOnly     bool   `toml:"unread_only"`
	PageSize       int    `toml:"page_size"`
	RefreshMinutes int    `toml:"refresh_minutes"`

	// Warnings are non-fatal problems found while loading.
	Warnings []string `toml:"-"`
}

// Defaults returns the default configuration.
func Defaults() Config {
	return Config{MarkReadOnOpen: true, UnreadOnly: true, PageSize: 50, RefreshMinutes: 5}
}

// DefaultPath returns the default config file location.
func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "vrotrss", "config.toml")
}

// runCommand executes password_cmd. It is a variable so tests can replace it.
var runCommand = func(ctx context.Context, command string) (string, error) {
	var cmd *exec.Cmd
	// The command comes from the user's own config file, so running it is intended.
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command) //nolint:gosec // password_cmd is the user's own config
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // password_cmd is the user's own config
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// Load reads the config file at path (or the default path when empty), applies
// environment overrides via getenv, resolves password_cmd and validates the
// result. A missing file is not an error by itself: the environment may supply
// everything.
func Load(path string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	explicit := path != ""
	if path == "" {
		path = DefaultPath()
	}
	cfg := Defaults()

	data, err := os.ReadFile(path) //nolint:gosec // path is user-provided by design
	switch {
	case err == nil:
		md, derr := toml.Decode(string(data), &cfg)
		if derr != nil {
			return cfg, fmt.Errorf("parse %s: %w", path, derr)
		}
		if und := md.Undecoded(); len(und) > 0 {
			keys := make([]string, len(und))
			for i, k := range und {
				keys[i] = k.String()
			}
			return cfg, fmt.Errorf("%s: unknown setting(s): %s", path, strings.Join(keys, ", "))
		}
		if cfg.Password != "" {
			if w := permWarning(path); w != "" {
				cfg.Warnings = append(cfg.Warnings, w)
			}
		}
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		// fine: environment variables may provide the settings
	default:
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if v := getenv("VROT_URL"); v != "" {
		cfg.URL = v
	}
	if v := getenv("VROT_USERNAME"); v != "" {
		cfg.Username = v
	}
	if v := getenv("VROT_PASSWORD"); v != "" {
		cfg.Password = v
	}

	if cfg.Password == "" && cfg.PasswordCmd != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, cerr := runCommand(ctx, cfg.PasswordCmd)
		if cerr != nil {
			return cfg, fmt.Errorf("password_cmd failed: %w", cerr)
		}
		cfg.Password = strings.TrimRight(out, "\r\n")
	}

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%w (config file: %s)", err, path)
	}
	return cfg, nil
}

// Validate checks that the configuration is usable.
func (c Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.URL) == "" {
		missing = append(missing, "url (or VROT_URL)")
	}
	if strings.TrimSpace(c.Username) == "" {
		missing = append(missing, "username (or VROT_USERNAME)")
	}
	if c.Password == "" {
		missing = append(missing, "password, password_cmd (or VROT_PASSWORD)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required setting(s): %s", strings.Join(missing, "; "))
	}
	if c.PageSize < 1 || c.PageSize > 500 {
		return fmt.Errorf("page_size must be between 1 and 500, got %d", c.PageSize)
	}
	if c.RefreshMinutes < 0 {
		return fmt.Errorf("refresh_minutes must be >= 0, got %d", c.RefreshMinutes)
	}
	return nil
}

func permWarning(path string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm()&0o077 == 0 {
		return ""
	}
	return fmt.Sprintf("%s contains a password but is readable by other users; run: chmod 600 %s", path, path)
}
