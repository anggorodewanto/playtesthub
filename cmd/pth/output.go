package main

import (
	"encoding/json"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Exit codes are the cli.md §8 contract — keep stable; the e2e suite
// asserts on them.
const (
	exitOK             = 0
	exitClientError    = 1 // gRPC InvalidArgument / NotFound / FailedPrecondition / etc.
	exitTransportError = 2 // gRPC Unavailable / DeadlineExceeded / network errors.
	exitLocalError     = 3 // Flag parse, env, file IO before any RPC.
)

// Status strings shared between meta-command JSON payloads (`doctor`) and
// flow-step NDJSON lines (`flow golden-m1`). Centralised so cross-file
// drift can't introduce subtle case differences.
const (
	statusOK     = "OK"
	statusFailed = "FAILED"
)

// exitCodeForGRPC maps a gRPC status code to the cli.md §8 exit code.
// The OK case is exitOK; anything Unavailable/DeadlineExceeded-shaped is
// exitTransportError; everything else (including Unknown for non-status
// errors) lands as exitClientError.
func exitCodeForGRPC(code codes.Code) int {
	switch code {
	case codes.OK:
		return exitOK
	case codes.Unavailable, codes.DeadlineExceeded:
		return exitTransportError
	default:
		return exitClientError
	}
}

// writeJSONProto marshals a proto message to stdout as a single-line JSON
// document with proto field names (cli.md §8). Adds a trailing newline so
// pipelines and `jq` work without surprises.
func writeJSONProto(w io.Writer, msg proto.Message) error {
	b, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal proto response: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// writeJSONValue marshals an arbitrary Go value to stdout as a single-line
// JSON document. Used by meta commands (`version`, `doctor`) whose payload
// is not a proto.
func writeJSONValue(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json value: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// writeGRPCError renders a gRPC failure to stderr in the cli.md §8 shape:
//
//	gRPC <CODE>: <message>\n
//
// Non-status errors (transport, context cancellation) are surfaced through
// status.FromError, which falls back to codes.Unknown — that path stays
// inside the exitClientError bucket per the mapping above.
func writeGRPCError(w io.Writer, err error) {
	if err == nil {
		return
	}
	st, _ := status.FromError(err)
	fmt.Fprintf(w, "gRPC %s: %s\n", st.Code(), st.Message())
}
