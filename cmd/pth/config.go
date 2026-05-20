package main

import (
	"context"
	"flag"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/proto"
)

func runPublicConfig(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("public-config", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}

	g.Anon = true
	req := &pb.GetPublicConfigRequest{}

	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetPublicConfig", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetPublicConfig(cctx, req)
		})
}
