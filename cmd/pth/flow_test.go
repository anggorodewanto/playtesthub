package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testFlowPlaytestID is the synthetic ULID handed back by the create
// stub. Reused across stop-on-failure / mismatch tests so the constant
// stays out of multiple string literals.
const testFlowPlaytestID = "01J0PLAYTESTID"

// flowFactoryRecorder pairs a per-profile stub with the dispatch seam
// expected by runFlow. Tests pre-load `byProfile` with a stub for each
// profile they exercise; the recorder panics if a profile is requested
// without a stub registered (catches "we wired the wrong profile" bugs).
type flowFactoryRecorder struct {
	byProfile map[string]*stubPlaytestClient
	requested []string
}

func (r *flowFactoryRecorder) factory(_ *Globals, profile string) (playtestClientFactory, *Globals) {
	r.requested = append(r.requested, profile)
	stub, ok := r.byProfile[profile]
	if !ok {
		panic("flow test: unregistered profile " + profile)
	}
	return factoryFor(stub), &Globals{Namespace: testNamespaceDev, Timeout: 5 * time.Second}
}

func newFlowGlobals() *Globals {
	return &Globals{
		Addr:      "localhost:6565",
		Namespace: testNamespaceDev,
		Timeout:   5 * time.Second,
	}
}

func TestRunFlowGoldenM1_RequiresSlug(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &flowFactoryRecorder{}
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--admin-profile", "a", "--player-profile", "p"}, rec.factory)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitLocalError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--slug is required") {
		t.Errorf("stderr=%q, want --slug message", stderr.String())
	}
}

func TestRunFlowGoldenM1_RequiresNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Timeout: 5 * time.Second}
	rec := &flowFactoryRecorder{}
	code := runFlow(t.Context(), &stdout, &stderr, g,
		[]string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "a", "--player-profile", "p"}, rec.factory)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--namespace") {
		t.Errorf("stderr=%q, want --namespace message", stderr.String())
	}
}

func TestRunFlowGoldenM1_RequiresProfiles(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing admin", []string{"golden-m1", "--slug", "demo-flow", "--player-profile", "p"}, "--admin-profile"},
		{"missing player", []string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "a"}, "--player-profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rec := &flowFactoryRecorder{}
			code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(), tc.args, rec.factory)
			if code != exitLocalError {
				t.Fatalf("exit=%d, want %d", code, exitLocalError)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr=%q, want %s", stderr.String(), tc.want)
			}
		})
	}
}

func TestRunFlowGoldenM1_UnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &flowFactoryRecorder{}
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(), []string{"golden-m9"}, rec.factory)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "unknown action") {
		t.Errorf("stderr=%q, want 'unknown action'", stderr.String())
	}
}

func TestRunFlowGoldenM1_DryRunEmitsAllSteps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &flowFactoryRecorder{} // empty: dry-run must not call the factory.
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--slug", "demo-flow", "--dry-run"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if len(rec.requested) != 0 {
		t.Errorf("dry-run requested factories: %v", rec.requested)
	}
	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 4; got != want {
		t.Fatalf("dry-run emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	wantSteps := []string{"create-playtest", "transition-open", "signup", "assert-pending"}
	for i, raw := range lines {
		var got struct {
			Step    string          `json:"step"`
			Status  string          `json:"status"`
			Request json.RawMessage `json:"request"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("line %d: %v: %q", i, err, raw)
		}
		if got.Step != wantSteps[i] {
			t.Errorf("line %d step=%q, want %q", i, got.Step, wantSteps[i])
		}
		if got.Status != "DRY_RUN" {
			t.Errorf("line %d status=%q, want DRY_RUN", i, got.Status)
		}
		if len(got.Request) == 0 {
			t.Errorf("line %d missing request body", i)
		}
	}
}

func TestRunFlowGoldenM1_HappyPath(t *testing.T) {
	createdID := testFlowPlaytestID
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			if in.Slug != "demo-flow" {
				t.Errorf("create slug=%q, want demo-flow", in.Slug)
			}
			if in.DistributionModel != pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS {
				t.Errorf("create distribution=%v, want STEAM_KEYS", in.DistributionModel)
			}
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Namespace: in.Namespace}}, nil
		},
		transitionFunc: func(_ context.Context, in *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("transition playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			if in.TargetStatus != pb.PlaytestStatus_PLAYTEST_STATUS_OPEN {
				t.Errorf("transition target=%v, want OPEN", in.TargetStatus)
			}
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, in *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			if in.Slug != "demo-flow" {
				t.Errorf("signup slug=%q, want demo-flow", in.Slug)
			}
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: "01J0APP", PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		applicantStatusFunc: func(_ context.Context, _ *pb.GetApplicantStatusRequest, _ ...grpc.CallOption) (*pb.GetApplicantStatusResponse, error) {
			return &pb.GetApplicantStatusResponse{Applicant: &pb.Applicant{Id: "01J0APP", Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}

	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 4; got != want {
		t.Fatalf("happy path emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	wantSteps := []string{"create-playtest", "transition-open", "signup", "assert-pending"}
	for i, raw := range lines {
		var got struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("line %d unmarshal: %v: %q", i, err, raw)
		}
		if got.Step != wantSteps[i] {
			t.Errorf("line %d step=%q, want %q", i, got.Step, wantSteps[i])
		}
		if got.Status != "OK" {
			t.Errorf("line %d status=%q, want OK", i, got.Status)
		}
	}

	// mk() is called once per profile (admin first, then player); each
	// returned factory is reused for that profile's two RPCs. The recorder
	// captures the mk() call sequence, not the underlying RPC sequence.
	if got, want := rec.requested, []string{"admin", "player"}; !equalSlices(got, want) {
		t.Errorf("profile factory sequence=%v, want %v", got, want)
	}
	if adminStub.calls != 2 {
		t.Errorf("admin stub calls=%d, want 2", adminStub.calls)
	}
	if playerStub.calls != 2 {
		t.Errorf("player stub calls=%d, want 2", playerStub.calls)
	}
}

func TestRunFlowGoldenM1_StopsOnTransitionFailure(t *testing.T) {
	createdID := testFlowPlaytestID
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return nil, status.Error(codes.FailedPrecondition, "playtest already CLOSED")
		},
	}
	playerStub := &stubPlaytestClient{} // must not be touched.
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitClientError, stderr.String())
	}

	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 2; got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	var second struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatalf("unmarshal failed line: %v", err)
	}
	if second.Step != "transition-open" || second.Status != statusFailed {
		t.Errorf("step/status=%q/%q, want transition-open/%s", second.Step, second.Status, statusFailed)
	}
	if second.Error.Code != codes.FailedPrecondition.String() {
		t.Errorf("error code=%q, want FailedPrecondition", second.Error.Code)
	}
	if !strings.Contains(second.Error.Message, "already CLOSED") {
		t.Errorf("error message=%q, want substring 'already CLOSED'", second.Error.Message)
	}
	if playerStub.calls != 0 {
		t.Errorf("player stub was called after transition failure: %d times", playerStub.calls)
	}
}

func TestRunFlowGoldenM1_AssertsPendingStatus(t *testing.T) {
	createdID := testFlowPlaytestID
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: "01J0APP", PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		applicantStatusFunc: func(_ context.Context, _ *pb.GetApplicantStatusRequest, _ ...grpc.CallOption) (*pb.GetApplicantStatusResponse, error) {
			// Wrong terminal state — flow must catch this.
			return &pb.GetApplicantStatusResponse{Applicant: &pb.Applicant{Id: "01J0APP", Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d", code, exitClientError)
	}

	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 5; got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	// 4th line is the success line for the assert-pending RPC; 5th line
	// is the synthetic FAILED that records the status mismatch.
	var fail struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(lines[4], &fail); err != nil {
		t.Fatalf("unmarshal mismatch line: %v", err)
	}
	if fail.Step != "assert-pending" || fail.Status != statusFailed {
		t.Errorf("step/status=%q/%q, want assert-pending/%s", fail.Step, fail.Status, statusFailed)
	}
	if !strings.Contains(fail.Error.Message, "PENDING") || !strings.Contains(fail.Error.Message, "APPROVED") {
		t.Errorf("mismatch message=%q, want both PENDING and APPROVED", fail.Error.Message)
	}
}

func TestRunFlowGoldenM1_FactoryDialFailureExitsTransport(t *testing.T) {
	failingFactory := func(_ *Globals, _ string) (playtestClientFactory, *Globals) {
		return func(_ context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error) {
				return nil, nil, nil, errors.New("connection refused")
			},
			newFlowGlobals()
	}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m1", "--slug", "demo-flow", "--admin-profile", "a", "--player-profile", "p"}, failingFactory)
	if code != exitTransportError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitTransportError, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	if len(lines) != 1 {
		t.Fatalf("emitted %d lines, want 1 FAILED line", len(lines))
	}
}

// splitNDJSON splits stdout on '\n', drops the trailing empty record, and
// returns each line as a raw byte slice. NDJSON readers don't tolerate an
// empty trailing line, but writeFlowSuccess emits one trailing newline
// per line so the buffer always ends with "\n".
func splitNDJSON(raw []byte) [][]byte {
	if len(raw) == 0 {
		return nil
	}
	parts := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
