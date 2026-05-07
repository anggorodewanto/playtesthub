package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/proto"
)

const auditUsage = `audit: action required (one of: list)`

// runAudit dispatches `pth audit <action> ...`. cli.md §6.3 (M3 phase 6).
// Today only `list` is wired — the M3 surface has a single audit RPC.
// Keeping the dispatcher shape lets later additions (export, filter
// presets) slot in without restructuring main.go.
func runAudit(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, auditUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case actionList:
		return runAuditList(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "audit: unknown action %q\n", action)
		return exitLocalError
	}
}

// runAuditList wraps ListAuditLog. Admin token required. --actor accepts
// the literal `system` (rows where actor_user_id IS NULL per PRD §4.7 /
// §5.7) or a UUID; the server enforces the shape. --action is exact
// match against the schema.md audit-action catalogue.
func runAuditList(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	actor := fs.String("actor", "", "actor filter: 'system' (system-emitted rows) or a UUID")
	action := fs.String("action", "", "action filter: exact match on the action string (schema.md audit-action catalogue)")
	cursor := fs.String("cursor", "", "opaque page_token from a prior response")
	pageSize := fs.Int("page-size", 0, "page size (0 → server default 50)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "audit list: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "audit list") {
		return exitLocalError
	}
	req := &pb.ListAuditLogRequest{
		Namespace:    g.Namespace,
		PlaytestId:   *playtestID,
		ActorFilter:  *actor,
		ActionFilter: *action,
		PageToken:    *cursor,
		PageSize:     int32(*pageSize),
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListAuditLog", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListAuditLog(cctx, req)
		})
}
