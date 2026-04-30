package main

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestRunVersion_EmitsRequiredFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runVersion(&stdout, &stderr, nil)
	if code != exitOK {
		t.Fatalf("runVersion exit=%d, want %d (stderr=%s)", code, exitOK, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty on success, got: %q", stderr.String())
	}
	var info versionInfo
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &info); err != nil {
		t.Fatalf("output not valid JSON: %v: %q", err, stdout.String())
	}
	if info.BuildSHA == "" {
		t.Fatal("buildSHA must be non-empty")
	}
	if info.GoVersion != runtime.Version() {
		t.Fatalf("goVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.ProtoSchemaID == "" {
		t.Fatal("protoSchema must be non-empty")
	}
	if info.ProtoFiles < 1 {
		t.Fatalf("protoFileCount must be >=1 (proto package compiled), got %d", info.ProtoFiles)
	}
}

func TestRunVersion_RejectsExtraArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runVersion(&stdout, &stderr, []string{"unexpected"})
	if code != exitLocalError {
		t.Fatalf("runVersion(extra) exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "unexpected") {
		t.Fatalf("stderr should mention rejected arg, got: %q", stderr.String())
	}
}
