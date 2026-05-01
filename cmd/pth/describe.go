package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
)

// describeOutput is the wire shape of `pth describe`. Schema is the cli.md
// §5 frozen identifier; agents branch on it.
type describeOutput struct {
	Schema   string        `json:"schema"`
	Commands []commandSpec `json:"commands"`
}

// runDescribe emits the catalogue JSON to stdout. Pretty-printed with two
// spaces (matches the checked-in golden file shape so the diff-check is a
// byte-exact comparison; cli.md §5 doesn't pin compactness, but agents
// piping through `jq` get the same payload either way).
func runDescribe(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "describe: unexpected argument %q\n", fs.Arg(0))
		return exitLocalError
	}
	out := describeOutput{
		Schema:   catalogueSchema,
		Commands: catalogue,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "describe: %v\n", err)
		return exitLocalError
	}
	if _, err := fmt.Fprintf(stdout, "%s\n", b); err != nil {
		fmt.Fprintf(stderr, "describe: %v\n", err)
		return exitLocalError
	}
	return exitOK
}
