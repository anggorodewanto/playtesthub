package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// doctorSentinelSlug — anything that cannot collide with a real playtest
// (PRD §5.1 slug regex `^[a-z0-9-]{3,50}$` allows this; using underscores
// is the safest sentinel because they're forbidden by the regex). Boot
// passes a real slug check is fine; this string is *intentionally*
// invalid so it always trips one of the validators.
//
// PRD slug rule: lowercased ASCII + digits + dash. Underscores fail the
// regex and produce InvalidArgument before the DB is touched. We accept
// either NotFound (regex was relaxed in some future revision) or
// InvalidArgument as proof the handler ran.
const doctorSentinelSlug = "__pth_doctor__"

type doctorReport struct {
	Status    string `json:"status"`             // "OK" on a reachable handler.
	Addr      string `json:"addr"`               // resolved gRPC endpoint.
	BasePath  string `json:"basePath,omitempty"` // echoed --base-path / PTH_BASE_PATH.
	LatencyMs int64  `json:"latencyMs"`          // round-trip (ms).
	GrpcCode  string `json:"grpcCode"`           // observed code on the sentinel call.
	Insecure  bool   `json:"insecure"`           // resolved insecure mode.
}

func runDoctor(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "doctor: unexpected argument %q\n", fs.Arg(0))
		return exitLocalError
	}

	g.Anon = true

	client, callCtx, closeFn, err := factory(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "doctor: dial %s: %v\n", g.Addr, err)
		return exitTransportError
	}
	defer closeFn() //nolint:errcheck

	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	callCtx, cancel := context.WithTimeout(callCtx, timeout)
	defer cancel()

	g.logRPC(stderr, "GetPublicPlaytest (doctor sentinel)")
	start := time.Now()
	_, rpcErr := client.GetPublicPlaytest(callCtx, &pb.GetPublicPlaytestRequest{Slug: doctorSentinelSlug})
	latency := time.Since(start)

	report := doctorReport{
		Addr:      g.Addr,
		BasePath:  g.BasePath,
		LatencyMs: latency.Milliseconds(),
		Insecure:  g.effectiveInsecure(),
	}

	st, _ := status.FromError(rpcErr)
	report.GrpcCode = st.Code().String()

	// A reachable handler returns NotFound (slug doesn't exist) or
	// InvalidArgument (sentinel fails slug-format validation upstream of
	// the DB). Either proves the gRPC route + service descriptor + handler
	// chain is wired. Anything else is a connectivity / config failure.
	switch st.Code() {
	case codes.NotFound, codes.InvalidArgument:
		report.Status = statusOK
		if err := writeJSONValue(stdout, report); err != nil {
			fmt.Fprintf(stderr, "doctor: %v\n", err)
			return exitLocalError
		}
		return exitOK
	case codes.OK:
		// Surprising but not strictly broken — a real playtest could
		// happen to use the sentinel slug. Treat as OK.
		report.Status = statusOK
		if err := writeJSONValue(stdout, report); err != nil {
			fmt.Fprintf(stderr, "doctor: %v\n", err)
			return exitLocalError
		}
		return exitOK
	default:
		report.Status = statusFailed
		writeGRPCError(stderr, rpcErr)
		_ = writeJSONValue(stdout, report)
		return exitCodeForGRPC(st.Code())
	}
}
