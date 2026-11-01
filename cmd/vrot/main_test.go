package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runArgs(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := runArgs(t, "-version")
	if code != 0 || !strings.HasPrefix(out, "vrot ") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestSampleConfigFlag(t *testing.T) {
	code, out, _ := runArgs(t, "-sample-config")
	if code != 0 || !strings.Contains(out, "password_cmd") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestBadFlagAndHelp(t *testing.T) {
	if code, _, _ := runArgs(t, "-nope"); code != 2 {
		t.Errorf("bad flag exit code = %d, want 2", code)
	}
	code, _, errOut := runArgs(t, "-h")
	if code != 0 || !strings.Contains(errOut, "VROT_URL") {
		t.Errorf("help: code=%d stderr=%q", code, errOut)
	}
}

func TestMissingConfigGivesHelpfulError(t *testing.T) {
	for _, k := range []string{"VROT_URL", "VROT_USERNAME", "VROT_PASSWORD"} {
		t.Setenv(k, "")
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte(`username = "u"`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runArgs(t, "-config", p)
	if code != 1 || !strings.Contains(errOut, "missing required") || !strings.Contains(errOut, "password_cmd") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestUnreachableServerFailsFast(t *testing.T) {
	t.Setenv("VROT_URL", "http://127.0.0.1:1")
	t.Setenv("VROT_USERNAME", "u")
	t.Setenv("VROT_PASSWORD", "p")
	p := filepath.Join(t.TempDir(), "none.toml")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runArgs(t, "-config", p)
	if code != 1 || !strings.Contains(errOut, "cannot log in") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}
