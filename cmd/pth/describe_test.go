package main

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

const goldenPath = "testdata/describe.golden.json"

func TestRunDescribe_EmitsCliSchemaV1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runDescribe(&stdout, &stderr, nil); code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got describeOutput
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Schema != "cli-schema.v1" {
		t.Errorf("schema=%q, want cli-schema.v1", got.Schema)
	}
	if len(got.Commands) == 0 {
		t.Fatal("commands empty")
	}
}

func TestRunDescribe_RejectsExtraArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runDescribe(&stdout, &stderr, []string{"unexpected"}); code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "unexpected") {
		t.Errorf("stderr=%q, want mention of the bad arg", stderr.String())
	}
}

func TestCatalogue_NamesAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(catalogue))
	for _, c := range catalogue {
		if _, dup := seen[c.Name]; dup {
			t.Errorf("duplicate command name %q", c.Name)
		}
		seen[c.Name] = struct{}{}
	}
}

func TestCatalogue_NamesAreSorted(t *testing.T) {
	names := make([]string, len(catalogue))
	for i, c := range catalogue {
		names[i] = c.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("commands are not alphabetised: %v", names)
	}
}

// TestCatalogue_AllEntriesAreSane catches careless edits: every entry
// must have a name, milestone, description, and example. Empty flag
// slices are fine (some commands take no flags) but the strings are
// load-bearing for AI consumers, so they have to be present.
func TestCatalogue_AllEntriesAreSane(t *testing.T) {
	for _, c := range catalogue {
		if c.Name == "" {
			t.Error("entry has empty name")
		}
		if c.Milestone == "" {
			t.Errorf("%s: milestone empty", c.Name)
		}
		if c.Description == "" {
			t.Errorf("%s: description empty", c.Name)
		}
		if c.Example == "" {
			t.Errorf("%s: example empty", c.Name)
		}
	}
}

// TestRunDescribe_MatchesGoldenFile is the cli.md §9 / STATUS.md M1
// phase 10.6 diff-check: regenerate the catalogue and compare against
// the checked-in golden. A mismatch means the registry changed and the
// golden file must be regenerated:
//
//	go run ./cmd/pth describe > cmd/pth/testdata/describe.golden.json
//
// CI runs this test on every PR — silent catalogue drift fails the build
// before AI consumers see it.
func TestRunDescribe_MatchesGoldenFile(t *testing.T) {
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := runDescribe(&stdout, &stderr, nil); code != exitOK {
		t.Fatalf("describe exit=%d (stderr=%q)", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Errorf("describe output drifted from %s.\nRegenerate via:\n  go run ./cmd/pth describe > %s\n\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, goldenPath, stdout.String(), string(want))
	}
}
