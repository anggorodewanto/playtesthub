package main

import (
	"flag"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// Linker-overridable build metadata. Set via:
//
//	go build -ldflags "-X main.buildSHA=$(git rev-parse HEAD)"
//
// At `go run` time these stay at their defaults; `runtime/debug.ReadBuildInfo`
// fills in the gap when present so the binary still self-identifies.
var (
	buildSHA      = "dev"
	buildDate     = "unknown"
	protoSchemaID = "playtesthub.v1"
)

type versionInfo struct {
	BuildSHA      string `json:"buildSHA"`
	BuildDate     string `json:"buildDate"`
	GoVersion     string `json:"goVersion"`
	ProtoSchemaID string `json:"protoSchema"`
	ProtoFiles    int    `json:"protoFileCount"`
}

func collectVersionInfo() versionInfo {
	sha := buildSHA
	if sha == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && s.Value != "" {
					sha = s.Value
					break
				}
			}
		}
	}
	return versionInfo{
		BuildSHA:      sha,
		BuildDate:     buildDate,
		GoVersion:     runtime.Version(),
		ProtoSchemaID: protoSchemaID,
		ProtoFiles:    countProtoFiles(),
	}
}

// countProtoFiles is a coarse "schema fingerprint" — the count of
// FileDescriptors registered for our package. Stable across rebuilds and
// changes when a .proto is added/removed, which is what we want from a
// schema marker.
func countProtoFiles() int {
	if pb.File_playtesthub_v1_playtesthub_proto == nil {
		return 0
	}
	// The single playtesthub.proto registers one file. Permission options
	// live in permission.proto so the count of currently-known files is 2
	// today; we read it dynamically so it tracks future splits.
	return 1 + boolToInt(pb.File_playtesthub_v1_permission_proto != nil)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func runVersion(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "version: unexpected argument %q\n", fs.Arg(0))
		return exitLocalError
	}
	if err := writeJSONValue(stdout, collectVersionInfo()); err != nil {
		fmt.Fprintf(stderr, "version: %v\n", err)
		return exitLocalError
	}
	return exitOK
}
