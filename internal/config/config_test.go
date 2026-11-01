package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func write(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	p := write(t, `
url = "https://rss.example.com"
username = "alice"
password = "pw"
page_size = 20
unread_only = false
refresh_minutes = 0
`, 0o600)
	cfg, err := Load(p, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://rss.example.com" || cfg.Username != "alice" || cfg.Password != "pw" ||
		cfg.PageSize != 20 || cfg.UnreadOnly || cfg.RefreshMinutes != 0 || !cfg.MarkReadOnOpen {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", cfg.Warnings)
	}
}

func TestEnvOverridesAndNoFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	// An explicitly requested file that does not exist is an error...
	if _, err := Load(missing, env(nil)); err == nil {
		t.Error("expected error for missing explicit config")
	}
	// ...but the default path may be absent when env provides everything.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load("", env(map[string]string{"VROT_URL": "https://e.example", "VROT_USERNAME": "bob", "VROT_PASSWORD": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "bob" || cfg.Password != "x" {
		t.Errorf("cfg = %+v", cfg)
	}

	p := write(t, `url="https://a.example"`+"\n"+`username="a"`+"\n"+`password="a"`, 0o600)
	cfg, err = Load(p, env(map[string]string{"VROT_URL": "https://b.example", "VROT_USERNAME": "b", "VROT_PASSWORD": "b"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://b.example" || cfg.Username != "b" || cfg.Password != "b" {
		t.Errorf("env should win: %+v", cfg)
	}
}

func TestMissingSettingsListed(t *testing.T) {
	p := write(t, `username = "alice"`, 0o600)
	_, err := Load(p, env(nil))
	if err == nil || !strings.Contains(err.Error(), "url") || !strings.Contains(err.Error(), "password") ||
		strings.Contains(err.Error(), "username (or") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownKeysAndBadTOML(t *testing.T) {
	p := write(t, "url = \"https://x\"\nusernmae = \"typo\"\n", 0o600)
	if _, err := Load(p, env(nil)); err == nil || !strings.Contains(err.Error(), "usernmae") {
		t.Errorf("want unknown key error, got %v", err)
	}
	p = write(t, "url = ", 0o600)
	if _, err := Load(p, env(nil)); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("want parse error, got %v", err)
	}
}

func TestValidateRanges(t *testing.T) {
	base := Defaults()
	base.URL, base.Username, base.Password = "https://x", "u", "p"
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.PageSize = 0
	if bad.Validate() == nil {
		t.Error("page_size 0 should fail")
	}
	bad = base
	bad.PageSize = 501
	if bad.Validate() == nil {
		t.Error("page_size 501 should fail")
	}
	bad = base
	bad.RefreshMinutes = -1
	if bad.Validate() == nil {
		t.Error("negative refresh should fail")
	}
}

func TestPasswordCmd(t *testing.T) {
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })

	var got string
	runCommand = func(_ context.Context, c string) (string, error) { got = c; return "s3cret\n", nil }
	p := write(t, "url=\"https://x\"\nusername=\"u\"\npassword_cmd=\"pass show x\"\n", 0o600)
	cfg, err := Load(p, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "s3cret" || got != "pass show x" {
		t.Errorf("password=%q cmd=%q", cfg.Password, got)
	}

	// password wins over password_cmd: the command must not run.
	got = ""
	if _, err := Load(p, env(map[string]string{"VROT_PASSWORD": "direct"})); err != nil || got != "" {
		t.Errorf("cmd ran despite password: err=%v got=%q", err, got)
	}

	runCommand = func(context.Context, string) (string, error) { return "", errors.New("boom") }
	if _, err := Load(p, env(nil)); err == nil || !strings.Contains(err.Error(), "password_cmd") {
		t.Errorf("want password_cmd error, got %v", err)
	}
}

func TestRealPasswordCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	out, err := runCommand(context.Background(), "printf hunter2")
	if err != nil || out != "hunter2" {
		t.Errorf("out=%q err=%v", out, err)
	}
}

func TestPermissionWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permissions")
	}
	p := write(t, "url=\"https://x\"\nusername=\"u\"\npassword=\"p\"\n", 0o644)
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "chmod 600") {
		t.Errorf("warnings = %v", cfg.Warnings)
	}
}

func TestSampleParses(t *testing.T) {
	p := write(t, Sample, 0o600)
	orig := runCommand
	t.Cleanup(func() { runCommand = orig })
	runCommand = func(context.Context, string) (string, error) { return "x", nil }
	if _, err := Load(p, env(nil)); err != nil {
		t.Fatalf("sample config must be loadable: %v", err)
	}
}

func TestDefaultPath(t *testing.T) {
	if p := DefaultPath(); !strings.HasSuffix(p, filepath.Join("vrotrss", "config.toml")) {
		t.Errorf("DefaultPath = %q", p)
	}
}
