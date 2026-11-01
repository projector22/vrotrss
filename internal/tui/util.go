package tui

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// fit truncates s to w display columns (adding an ellipsis) and pads it with
// spaces so the result is exactly w columns wide.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// padLeft right-aligns s within w columns.
func padLeft(s string, w int) string {
	if sw := ansi.StringWidth(s); sw < w {
		return strings.Repeat(" ", w-sw) + s
	}
	return fit(s, w)
}

// clamp limits v to [lo, hi].
func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	return min(max(v, lo), hi)
}

// scrollTo returns the offset that keeps cursor visible in a window of height h.
func scrollTo(cursor, offset, h, total int) int {
	if h <= 0 || total <= h {
		return 0
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+h {
		offset = cursor - h + 1
	}
	return clamp(offset, 0, total-h)
}

// shortDate formats t relative to now, FreshRSS-list style.
func shortDate(t, now time.Time, loc *time.Location) string {
	t, now = t.In(loc), now.In(loc)
	switch {
	case t.IsZero() || t.Unix() == 0:
		return ""
	case t.Year() == now.Year() && t.YearDay() == now.YearDay():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	default:
		return t.Format("2 Jan 2006")
	}
}

// count formats an unread count compactly.
func count(n int) string {
	switch {
	case n <= 0:
		return ""
	case n > 9999:
		return "9999+"
	default:
		return fmt.Sprint(n)
	}
}

func validWebURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// OpenBrowser opens an http(s) URL in the user's default browser.
func OpenBrowser(raw string) error {
	raw = strings.TrimSpace(raw)
	if !validWebURL(raw) {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", raw)
	}
	name, args := "xdg-open", []string{raw}
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", raw}
	}
	// raw was validated above to be an http(s) URL, so it cannot be parsed as a flag.
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // validated http(s) URL
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
