package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runUser dispatches `pth user <action> ...`. All actions hit AGS IAM
// admin endpoints and need an admin-credentialed active profile, so the
// dispatcher resolves the bearer up front and forwards it.
func runUser(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, getenv envSnapshot) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "user: action required (one of: create, delete, login-as)")
		return exitLocalError
	}
	deps, err := defaultAuthDeps(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "user: %v\n", err)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "create":
		return runUserCreate(ctx, stdout, stderr, g, rest, deps)
	case "delete":
		return runUserDelete(ctx, stdout, stderr, g, rest, deps)
	case "login-as":
		return runUserLoginAs(ctx, stdout, stderr, g, rest, deps)
	default:
		fmt.Fprintf(stderr, "user: unknown action %q\n", action)
		return exitLocalError
	}
}

// runUserCreate POSTs to /iam/v4/admin/namespaces/{ns}/test_users and
// echoes the AGS-generated creds. Single user → one JSON object; count>1 →
// an array, so jq-style consumers can branch on shape.
func runUserCreate(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	count := fs.Int("count", 1, "number of test users to create (AGS max 100)")
	country := fs.String("country", "US", "ISO3166-1 alpha-2 country code")
	dryRun := fs.Bool("dry-run", false, "print the request body and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *count <= 0 {
		fmt.Fprintln(stderr, "user create: --count must be >= 1")
		return exitLocalError
	}
	if *count > 100 {
		fmt.Fprintln(stderr, "user create: --count must be <= 100 (AGS limit)")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "user create: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	body := &adminCreateTestUsersRequest{
		Count:    *count,
		UserInfo: &adminTestUserInfo{Country: *country},
	}
	if *dryRun {
		if err := writeJSONValue(stdout, map[string]any{
			"method":    "POST",
			"path":      fmt.Sprintf(adminCreateTestUsersPath, g.Namespace),
			"body":      body,
			"namespace": g.Namespace,
		}); err != nil {
			fmt.Fprintf(stderr, "user create: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}
	bearer, code := requireAdminBearer(ctx, stderr, g, deps, "user create")
	if code != exitOK {
		return code
	}
	resp, err := deps.iam.adminCreateTestUsers(ctx, bearer, g.Namespace, body)
	if err != nil {
		return reportAuthFailure(stderr, "user create", err)
	}
	if *count == 1 {
		u := resp.Data[0]
		if err := writeJSONValue(stdout, testUserToJSON(u)); err != nil {
			fmt.Fprintf(stderr, "user create: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}
	out := make([]map[string]any, 0, len(resp.Data))
	for _, u := range resp.Data {
		out = append(out, testUserToJSON(u))
	}
	if err := writeJSONValue(stdout, out); err != nil {
		fmt.Fprintf(stderr, "user create: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

func testUserToJSON(u *adminCreateTestUserResponse) map[string]any {
	return map[string]any{
		"userId":       u.UserID,
		"username":     u.Username,
		"password":     u.Password,
		"emailAddress": u.EmailAddress,
		"namespace":    u.Namespace,
	}
}

// runUserDelete prompts for confirmation unless --yes is supplied, so a
// scripted invocation sets --yes explicitly and a stray manual run gets a
// guard.
func runUserDelete(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("user delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "AGS user id to delete (required)")
	yes := fs.Bool("yes", false, "skip the destructive-confirm prompt")
	dryRun := fs.Bool("dry-run", false, "print the target URL and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "user delete: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "user delete: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	if *dryRun {
		if err := writeJSONValue(stdout, map[string]any{
			"method":    "DELETE",
			"path":      fmt.Sprintf(adminDeleteUserPath, g.Namespace, *id),
			"namespace": g.Namespace,
			"userId":    *id,
		}); err != nil {
			fmt.Fprintf(stderr, "user delete: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}
	if !*yes {
		// Reject the run rather than blocking on stdin: scripted callers
		// can't distinguish a hung CLI from a slow request, and a stray
		// human invocation gets the cli.md §6.1 guard either way.
		fmt.Fprintln(stderr, "user delete: refusing to delete without --yes (this is destructive)")
		return exitLocalError
	}
	bearer, code := requireAdminBearer(ctx, stderr, g, deps, "user delete")
	if code != exitOK {
		return code
	}
	if err := deps.iam.adminDeleteUser(ctx, bearer, g.Namespace, *id); err != nil {
		return reportAuthFailure(stderr, "user delete", err)
	}
	if err := writeJSONValue(stdout, map[string]any{
		"userId":  *id,
		"deleted": true,
	}); err != nil {
		fmt.Fprintf(stderr, "user delete: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

// runUserLoginAs resolves userId → username via admin GET, then runs the
// existing ROPC grant under --profile. The caller-supplied password is
// the AGS-generated one from `user create`'s stdout.
func runUserLoginAs(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("user login-as", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "AGS user id to login as (required)")
	stdinPw := fs.Bool("password-stdin", false, "read password from one line on stdin instead of TTY prompt")
	dryRun := fs.Bool("dry-run", false, "print the target URLs and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "user login-as: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "user login-as: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	if *dryRun {
		if err := writeJSONValue(stdout, map[string]any{
			"lookupPath": fmt.Sprintf(adminGetUserPath, g.Namespace, *id),
			"tokenPath":  iamTokenPath,
			"namespace":  g.Namespace,
			"userId":     *id,
			"profile":    g.Profile,
		}); err != nil {
			fmt.Fprintf(stderr, "user login-as: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}
	bearer, code := requireAdminBearer(ctx, stderr, g, deps, "user login-as")
	if code != exitOK {
		return code
	}
	user, err := deps.iam.adminGetUserByID(ctx, bearer, g.Namespace, *id)
	if err != nil {
		return reportAuthFailure(stderr, "user login-as", err)
	}
	pw, err := readLoginPassword(deps, *stdinPw, user.Username)
	if err != nil {
		fmt.Fprintf(stderr, "user login-as: %v\n", err)
		return exitLocalError
	}
	if pw == "" {
		fmt.Fprintln(stderr, "user login-as: empty password")
		return exitLocalError
	}
	tok, err := deps.iam.passwordLogin(ctx, g.Namespace, user.Username, pw, deps.now)
	if err != nil {
		return reportAuthFailure(stderr, "user login-as", err)
	}
	if err := deps.store.putProfile(g.Profile, &profileEntry{
		Addr:         g.Addr,
		Namespace:    g.Namespace,
		UserID:       firstNonEmpty(tok.UserID, *id),
		LoginMode:    loginModePassword,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.ExpiresAt,
	}); err != nil {
		fmt.Fprintf(stderr, "user login-as: storing credential: %v\n", err)
		return exitLocalError
	}
	if err := writeJSONValue(stdout, map[string]any{
		"profile":   g.Profile,
		"userId":    firstNonEmpty(tok.UserID, *id),
		"username":  user.Username,
		"namespace": g.Namespace,
		"loginMode": loginModePassword,
		"expiresAt": tok.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}); err != nil {
		fmt.Fprintf(stderr, "user login-as: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

// requireAdminBearer resolves the active profile's access token (refreshing
// if near-expiry) and returns it as the bearer for admin REST calls. When
// no profile is stored, returns a clear error pointing the user at
// `pth auth login --password`.
func requireAdminBearer(ctx context.Context, stderr io.Writer, g *Globals, deps *authDeps, prefix string) (string, int) {
	p, err := resolveActiveProfile(ctx, g, deps)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
		return "", exitLocalError
	}
	if p == nil || p.AccessToken == "" {
		fmt.Fprintf(stderr, "%s: no credential for profile %q (run: pth auth login --password as an admin user)\n", prefix, g.Profile)
		return "", exitLocalError
	}
	return strings.TrimSpace(p.AccessToken), exitOK
}
