// Command vrot is a terminal UI for FreshRSS (vrotrss).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vrotrss/vrotrss/internal/config"
	"github.com/vrotrss/vrotrss/internal/demo"
	"github.com/vrotrss/vrotrss/internal/greader"
	"github.com/vrotrss/vrotrss/internal/tui"
)

// Set at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vrot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "", "path to config file (default "+config.DefaultPath()+")")
	showVersion := fs.Bool("version", false, "print version and exit")
	useDemo := fs.Bool("demo", false, "run with built-in sample data (no server needed)")
	sample := fs.Bool("sample-config", false, "print an example config file and exit")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "vrot — a terminal UI for FreshRSS\n\nUsage: vrot [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(stderr, "\nEnvironment: VROT_URL, VROT_USERNAME, VROT_PASSWORD override the config file.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	switch {
	case *showVersion:
		_, _ = fmt.Fprintf(stdout, "vrot %s (commit %s, built %s)\n", version, commit, date)
		return 0
	case *sample:
		_, _ = fmt.Fprint(stdout, config.Sample)
		return 0
	}

	opts := tui.Options{Version: version, MarkReadOnOpen: true, UnreadOnly: true, PageSize: 50, RefreshEvery: 5 * time.Minute}
	var api tui.API
	if *useDemo {
		api = demo.New(time.Now())
	} else {
		cfg, err := config.Load(*cfgPath, os.Getenv)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "vrot: %v\n\nCreate %s, for example:\n\n%s\nSee `vrot -h` for more.\n",
				err, config.DefaultPath(), config.Sample)
			return 1
		}
		for _, w := range cfg.Warnings {
			_, _ = fmt.Fprintf(stderr, "vrot: warning: %s\n", w)
		}
		client, err := greader.New(cfg.URL, cfg.Username, cfg.Password)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "vrot: %v\n", err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = client.Login(ctx)
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "vrot: cannot log in to %s: %v\n", cfg.URL, err)
			return 1
		}
		api = client
		opts.MarkReadOnOpen, opts.UnreadOnly, opts.PageSize = cfg.MarkReadOnOpen, cfg.UnreadOnly, cfg.PageSize
		opts.RefreshEvery = time.Duration(cfg.RefreshMinutes) * time.Minute
	}

	p := tea.NewProgram(tui.New(api, opts), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		_, _ = fmt.Fprintf(stderr, "vrot: %v\n", err)
		return 1
	}
	return 0
}
