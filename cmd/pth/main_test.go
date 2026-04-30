package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_NoArgsShowsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, nil, emptyEnv)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("usage not printed to stderr: %q", stderr.String())
	}
}

func TestRun_HelpAlias(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{"help"}, emptyEnv)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("usage not printed to stdout: %q", stdout.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{"banana"}, emptyEnv)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "banana") {
		t.Errorf("stderr should name the unknown command, got %q", stderr.String())
	}
}

func TestRun_VersionWiringEnd2End(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{"version"}, emptyEnv)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "buildSHA") {
		t.Errorf("version output missing buildSHA: %q", stdout.String())
	}
}

func TestRun_GlobalFlagBeforeUnknownCommand(t *testing.T) {
	// Smoke test: --addr is consumed by the global parser; the next token
	// is the (unknown) command. Confirms the global parser stops at the
	// first non-flag positional.
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{"--addr", "localhost:9999", "banana"}, emptyEnv)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "banana") {
		t.Errorf("stderr should name unknown command, got %q", stderr.String())
	}
}

func TestRun_ParseErrorIsExit3(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), &stdout, &stderr, []string{"--timeout", "garbage"}, emptyEnv)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}
