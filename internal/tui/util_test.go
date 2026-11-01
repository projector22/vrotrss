package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestFitAndPadLeft(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcdef", 4, "abc…"},
		{"日本語です", 6, ""},
		{"x", 0, ""},
		{"x", -3, ""},
	}
	for _, tt := range tests {
		got := fit(tt.in, tt.w)
		if tt.w > 0 && ansi.StringWidth(got) != tt.w {
			t.Errorf("fit(%q,%d) = %q (width %d)", tt.in, tt.w, got, ansi.StringWidth(got))
		}
		if tt.w <= 0 && got != "" {
			t.Errorf("fit(%q,%d) = %q, want empty", tt.in, tt.w, got)
		}
	}
	if got := fit("abcdef", 4); got != "abc…" {
		t.Errorf("fit truncation = %q", got)
	}
	if got := padLeft("42", 5); got != "   42" {
		t.Errorf("padLeft = %q", got)
	}
	if got := padLeft("toolong", 4); ansi.StringWidth(got) != 4 {
		t.Errorf("padLeft overflow = %q", got)
	}
}

func TestClamp(t *testing.T) {
	for _, tt := range []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5}, {-1, 0, 10, 0}, {11, 0, 10, 10}, {3, 0, -1, 0},
	} {
		if got := clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("clamp(%d,%d,%d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

func TestScrollTo(t *testing.T) {
	tests := []struct{ cursor, offset, h, total, want int }{
		{0, 0, 10, 5, 0},    // everything fits
		{3, 0, 5, 20, 0},    // visible
		{7, 0, 5, 20, 3},    // below window
		{1, 4, 5, 20, 1},    // above window
		{19, 0, 5, 20, 15},  // end
		{10, 99, 5, 20, 10}, // stale offset
		{0, 0, 0, 20, 0},    // zero height
	}
	for _, tt := range tests {
		if got := scrollTo(tt.cursor, tt.offset, tt.h, tt.total); got != tt.want {
			t.Errorf("scrollTo(%+v) = %d, want %d", tt, got, tt.want)
		}
	}
}

func TestShortDate(t *testing.T) {
	now := time.Date(2026, 11, 1, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC), "09:05"},
		{time.Date(2026, 10, 31, 23, 59, 0, 0, time.UTC), "31 Oct"},
		{time.Date(2025, 12, 25, 8, 0, 0, 0, time.UTC), "25 Dec 2025"},
		{time.Unix(0, 0), ""},
		{time.Time{}, ""},
	}
	for _, tt := range tests {
		if got := shortDate(tt.t, now, time.UTC); got != tt.want {
			t.Errorf("shortDate(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
	// Day boundaries follow the configured location, not UTC.
	loc := time.FixedZone("X", 10*3600)
	late := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC) // 06:00 on 2 Nov in loc
	if got := shortDate(late, time.Date(2026, 11, 2, 7, 0, 0, 0, loc), loc); got != "06:00" {
		t.Errorf("location-aware shortDate = %q", got)
	}
}

func TestCount(t *testing.T) {
	for in, want := range map[int]string{-1: "", 0: "", 7: "7", 9999: "9999", 10000: "9999+"} {
		if got := count(in); got != want {
			t.Errorf("count(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestValidWebURL(t *testing.T) {
	good := []string{"https://example.com/a?b=c", "http://localhost:8080", " https://example.com "}
	bad := []string{
		"", "javascript:alert(1)", "file:///etc/passwd", "ftp://x.example", "-oProxyCommand=evil",
		"example.com", "https://", "http://exa mple.com/\x7f",
	}
	for _, u := range good {
		if !validWebURL(u) {
			t.Errorf("validWebURL(%q) = false", u)
		}
	}
	for _, u := range bad {
		if validWebURL(u) {
			t.Errorf("validWebURL(%q) = true", u)
		}
	}
}

func TestOpenBrowserRefusesNonWebURLs(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "--help", ""} {
		err := OpenBrowser(u)
		if err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("OpenBrowser(%q) err = %v, want refusal", u, err)
		}
	}
}
