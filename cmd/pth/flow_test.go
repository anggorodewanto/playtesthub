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
		if got.Status != statusDryRun {
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

// ---------------- golden-m2 ----------------

func TestRunFlowGoldenM2_HappyPath(t *testing.T) {
	createdID := testFlowPlaytestID
	applicantID := "01J0M2APP"
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			if !in.NdaRequired {
				t.Errorf("create nda_required=%v, want true", in.NdaRequired)
			}
			if in.NdaText == "" {
				t.Errorf("create nda_text empty")
			}
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Namespace: in.Namespace}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, in *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("upload playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			if !strings.Contains(in.CsvContent, "GOLDEN-M2-DEMO-FLOW-M2-0000") {
				t.Errorf("upload csv body=%q, want synthesised slug entry", in.CsvContent)
			}
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		approveFunc: func(_ context.Context, in *pb.ApproveApplicantRequest, _ ...grpc.CallOption) (*pb.ApproveApplicantResponse, error) {
			if in.ApplicantId != applicantID {
				t.Errorf("approve applicant_id=%q, want %q", in.ApplicantId, applicantID)
			}
			return &pb.ApproveApplicantResponse{Applicant: &pb.Applicant{Id: applicantID, Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, in *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: applicantID, PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING, Platforms: in.Platforms}}, nil
		},
		acceptNDAFunc: func(_ context.Context, in *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("accept-nda playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
		getGrantedCodeFunc: func(_ context.Context, _ *pb.GetGrantedCodeRequest, _ ...grpc.CallOption) (*pb.GetGrantedCodeResponse, error) {
			return &pb.GetGrantedCodeResponse{Value: "STEAM-KEY-1", DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q stdout=%q)", code, exitOK, stderr.String(), stdout.String())
	}

	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{"create-playtest", "transition-open", "signup", "accept-nda", "upload-codes", "approve", "get-code"}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
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
}

func TestRunFlowGoldenM2_StopsOnApproveFailure(t *testing.T) {
	createdID := testFlowPlaytestID
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, _ *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		approveFunc: func(_ context.Context, _ *pb.ApproveApplicantRequest, _ ...grpc.CallOption) (*pb.ApproveApplicantResponse, error) {
			return nil, status.Error(codes.ResourceExhausted, "No codes remaining in pool. Upload more codes to continue approving.")
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: "01J0APP", PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		acceptNDAFunc: func(_ context.Context, _ *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitClientError, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	// 5 OK lines + 1 FAILED (approve). get-code must NOT execute.
	if got, want := len(lines), 6; got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	var last struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(lines[5], &last); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if last.Step != "approve" || last.Status != statusFailed {
		t.Errorf("got %q/%q, want approve/%s", last.Step, last.Status, statusFailed)
	}
	if last.Error.Code != codes.ResourceExhausted.String() {
		t.Errorf("code=%q, want ResourceExhausted", last.Error.Code)
	}
}

func TestRunFlowGoldenM2_DryRun(t *testing.T) {
	rec := &flowFactoryRecorder{}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2", "--dry-run"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 7; got != want {
		t.Fatalf("dry-run emitted %d lines, want %d", got, want)
	}
	if len(rec.requested) != 0 {
		t.Errorf("dry-run must not request profiles: %v", rec.requested)
	}
}

func TestRunFlowGoldenM2_RequiresProfilesWhenNotDryRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &flowFactoryRecorder{}
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2"}, rec.factory)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--admin-profile") {
		t.Errorf("stderr=%q, want --admin-profile", stderr.String())
	}
}

// M5.A phase 5: --auto-approve hoists upload-codes before signup and
// replaces the manual approve step with assert-applicant-auto-approved.
func TestRunFlowGoldenM2_AutoApproveDryRunReorders(t *testing.T) {
	rec := &flowFactoryRecorder{}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2-aa",
			"--auto-approve", "--auto-approve-limit", "5", "--dry-run"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q stdout=%q)", code, exitOK, stderr.String(), stdout.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{
		"create-playtest", "transition-open", "upload-codes",
		"signup", "accept-nda", "assert-applicant-auto-approved", "get-code",
	}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	for i, raw := range lines {
		var got struct {
			Step    string          `json:"step"`
			Status  string          `json:"status"`
			Request json.RawMessage `json:"request"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("line %d unmarshal: %v: %q", i, err, raw)
		}
		if got.Step != wantSteps[i] {
			t.Errorf("line %d step=%q, want %q", i, got.Step, wantSteps[i])
		}
		if got.Status != statusDryRun {
			t.Errorf("line %d status=%q, want DRY_RUN", i, got.Status)
		}
	}
	// create-playtest body must carry auto_approve=true + the cap.
	var first struct {
		Request struct {
			AutoApprove      *bool   `json:"auto_approve"`
			AutoApproveLimit *int32  `json:"auto_approve_limit"`
			NdaText          *string `json:"nda_text"`
		} `json:"request"`
	}
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatalf("unmarshal create-playtest line: %v", err)
	}
	if first.Request.AutoApprove == nil || !*first.Request.AutoApprove {
		t.Errorf("create-playtest auto_approve missing or false: %s", lines[0])
	}
	if first.Request.AutoApproveLimit == nil || *first.Request.AutoApproveLimit != 5 {
		t.Errorf("create-playtest auto_approve_limit missing or != 5: %s", lines[0])
	}
}

func TestRunFlowGoldenM2_AutoApproveHappyPath(t *testing.T) {
	createdID := testFlowPlaytestID
	applicantID := "01J0M2AA"
	uploadBeforeSignup := false
	signupSeen := false
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			if !in.AutoApprove {
				t.Errorf("create auto_approve=%v, want true", in.AutoApprove)
			}
			if in.AutoApproveLimit == nil || *in.AutoApproveLimit != 5 {
				t.Errorf("create auto_approve_limit=%v, want 5", in.AutoApproveLimit)
			}
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Namespace: in.Namespace}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, _ *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			if signupSeen {
				t.Errorf("upload-codes ran after signup (auto-approve variant must hoist upload before signup)")
			} else {
				uploadBeforeSignup = true
			}
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		approveFunc: func(_ context.Context, _ *pb.ApproveApplicantRequest, _ ...grpc.CallOption) (*pb.ApproveApplicantResponse, error) {
			t.Fatal("auto-approve variant must NOT call ApproveApplicant")
			return nil, nil
		},
		listApplicantsFunc: func(_ context.Context, in *pb.ListApplicantsRequest, _ ...grpc.CallOption) (*pb.ListApplicantsResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("list-applicants playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			return &pb.ListApplicantsResponse{Applicants: []*pb.Applicant{{
				Id: applicantID, PlaytestId: createdID,
				Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED, AutoApproved: true,
			}}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			signupSeen = true
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: applicantID, PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED}}, nil
		},
		acceptNDAFunc: func(_ context.Context, _ *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
		getGrantedCodeFunc: func(_ context.Context, _ *pb.GetGrantedCodeRequest, _ ...grpc.CallOption) (*pb.GetGrantedCodeResponse, error) {
			return &pb.GetGrantedCodeResponse{Value: "STEAM-KEY-AA-1", DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2-aa",
			"--auto-approve", "--auto-approve-limit", "5",
			"--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q stdout=%q)", code, exitOK, stderr.String(), stdout.String())
	}
	if !uploadBeforeSignup {
		t.Fatal("expected upload-codes to run before signup")
	}
	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{
		"create-playtest", "transition-open", "upload-codes",
		"signup", "accept-nda", "assert-applicant-auto-approved", "get-code",
	}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
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
}

func TestRunFlowGoldenM2_AutoApproveAssertionFailsOnPending(t *testing.T) {
	createdID := testFlowPlaytestID
	applicantID := "01J0M2AAPENDING"
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, _ *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		listApplicantsFunc: func(_ context.Context, _ *pb.ListApplicantsRequest, _ ...grpc.CallOption) (*pb.ListApplicantsResponse, error) {
			// Pool was empty → silent PENDING fallback. assert step must fail.
			return &pb.ListApplicantsResponse{Applicants: []*pb.Applicant{{
				Id: applicantID, PlaytestId: createdID,
				Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING, AutoApproved: false,
			}}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: applicantID, PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		acceptNDAFunc: func(_ context.Context, _ *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m2", "--slug", "demo-flow-m2-aa-fail",
			"--auto-approve", "--auto-approve-limit", "5",
			"--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitClientError, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	// 6 OK (the ListApplicants RPC succeeds — it's the *post-RPC* data
	// check that fails) + 1 synthetic FAILED on the same step. get-code
	// must NOT run. Mirrors the golden-m1 assert-pending FAILED shape.
	if got, want := len(lines), 7; got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	var last struct {
		Step   string `json:"step"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(lines[6], &last); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if last.Step != "assert-applicant-auto-approved" || last.Status != statusFailed {
		t.Errorf("got %q/%q, want assert-applicant-auto-approved/%s", last.Step, last.Status, statusFailed)
	}
}

func TestRunFlowGoldenM3_HappyPath(t *testing.T) {
	createdID := testFlowPlaytestID
	applicantID := "01J0M3APP"
	surveyID := "01J0M3SURVEY"
	textQID := "01J0M3QT"
	ratingQID := "01J0M3QR"

	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Namespace: in.Namespace}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, _ *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		approveFunc: func(_ context.Context, _ *pb.ApproveApplicantRequest, _ ...grpc.CallOption) (*pb.ApproveApplicantResponse, error) {
			return &pb.ApproveApplicantResponse{Applicant: &pb.Applicant{Id: applicantID, Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED}}, nil
		},
		createSurveyFunc: func(_ context.Context, in *pb.CreateSurveyRequest, _ ...grpc.CallOption) (*pb.CreateSurveyResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("create-survey playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			if got := len(in.Questions); got != 2 {
				t.Errorf("create-survey questions len=%d, want 2", got)
			}
			// Server normally assigns ids; the stub mints them so the
			// follow-up submit-response can address each question.
			questions := []*pb.SurveyQuestion{
				{Id: textQID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT, Prompt: in.Questions[0].Prompt, Required: true},
				{Id: ratingQID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING, Prompt: in.Questions[1].Prompt, Required: true},
			}
			return &pb.CreateSurveyResponse{Survey: &pb.Survey{Id: surveyID, PlaytestId: createdID, Version: 1, Questions: questions}}, nil
		},
		listSurveyRespFunc: func(_ context.Context, in *pb.ListSurveyResponsesRequest, _ ...grpc.CallOption) (*pb.ListSurveyResponsesResponse, error) {
			if in.PlaytestId != createdID {
				t.Errorf("list-responses playtest_id=%q, want %q", in.PlaytestId, createdID)
			}
			return &pb.ListSurveyResponsesResponse{Responses: []*pb.SurveyResponse{{
				Id: "01J0M3RESP", PlaytestId: createdID, SurveyId: surveyID,
			}}}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: applicantID, PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		acceptNDAFunc: func(_ context.Context, _ *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
		getGrantedCodeFunc: func(_ context.Context, _ *pb.GetGrantedCodeRequest, _ ...grpc.CallOption) (*pb.GetGrantedCodeResponse, error) {
			return &pb.GetGrantedCodeResponse{Value: "STEAM-KEY-1", DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS}, nil
		},
		submitSurveyFunc: func(_ context.Context, in *pb.SubmitSurveyResponseRequest, _ ...grpc.CallOption) (*pb.SubmitSurveyResponseResponse, error) {
			if in.SurveyId != surveyID {
				t.Errorf("submit survey_id=%q, want %q", in.SurveyId, surveyID)
			}
			if got := len(in.Answers); got != 2 {
				t.Fatalf("submit answers len=%d, want 2", got)
			}
			if in.Answers[0].QuestionId != textQID || in.Answers[0].GetText() == "" {
				t.Errorf("answers[0]=%+v, want text answer for %q", in.Answers[0], textQID)
			}
			if in.Answers[1].QuestionId != ratingQID || in.Answers[1].GetRating() != 5 {
				t.Errorf("answers[1]=%+v, want rating 5 for %q", in.Answers[1], ratingQID)
			}
			return &pb.SubmitSurveyResponseResponse{Response: &pb.SurveyResponse{Id: "01J0M3RESP", PlaytestId: createdID, SurveyId: surveyID}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m3", "--slug", "demo-flow-m3", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q stdout=%q)", code, exitOK, stderr.String(), stdout.String())
	}

	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{"create-playtest", "transition-open", "signup", "accept-nda", "upload-codes", "approve", "get-code", "create-survey", "submit-response", "list-responses"}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
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
}

func TestRunFlowGoldenM3_StopsOnEmptyResponses(t *testing.T) {
	createdID := testFlowPlaytestID
	applicantID := "01J0M3APP"
	surveyID := "01J0M3SURVEY"
	textQID := "01J0M3QT"
	ratingQID := "01J0M3QR"

	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug}}, nil
		},
		transitionFunc: func(_ context.Context, _ *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
		},
		uploadCodesFunc: func(_ context.Context, _ *pb.UploadCodesRequest, _ ...grpc.CallOption) (*pb.UploadCodesResponse, error) {
			return &pb.UploadCodesResponse{Inserted: 1}, nil
		},
		approveFunc: func(_ context.Context, _ *pb.ApproveApplicantRequest, _ ...grpc.CallOption) (*pb.ApproveApplicantResponse, error) {
			return &pb.ApproveApplicantResponse{Applicant: &pb.Applicant{Id: applicantID, Status: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED}}, nil
		},
		createSurveyFunc: func(_ context.Context, in *pb.CreateSurveyRequest, _ ...grpc.CallOption) (*pb.CreateSurveyResponse, error) {
			questions := []*pb.SurveyQuestion{
				{Id: textQID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT, Prompt: in.Questions[0].Prompt, Required: true},
				{Id: ratingQID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING, Prompt: in.Questions[1].Prompt, Required: true},
			}
			return &pb.CreateSurveyResponse{Survey: &pb.Survey{Id: surveyID, Questions: questions}}, nil
		},
		listSurveyRespFunc: func(_ context.Context, _ *pb.ListSurveyResponsesRequest, _ ...grpc.CallOption) (*pb.ListSurveyResponsesResponse, error) {
			// The server returns OK but with zero rows — the flow's
			// post-condition should reject this as the submit step
			// should have produced exactly one row.
			return &pb.ListSurveyResponsesResponse{}, nil
		},
	}
	playerStub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: applicantID, PlaytestId: createdID, Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
		acceptNDAFunc: func(_ context.Context, _ *pb.AcceptNDARequest, _ ...grpc.CallOption) (*pb.AcceptNDAResponse, error) {
			return &pb.AcceptNDAResponse{Acceptance: &pb.NDAAcceptance{NdaVersionHash: "h"}}, nil
		},
		getGrantedCodeFunc: func(_ context.Context, _ *pb.GetGrantedCodeRequest, _ ...grpc.CallOption) (*pb.GetGrantedCodeResponse, error) {
			return &pb.GetGrantedCodeResponse{Value: "STEAM-KEY-1"}, nil
		},
		submitSurveyFunc: func(_ context.Context, _ *pb.SubmitSurveyResponseRequest, _ ...grpc.CallOption) (*pb.SubmitSurveyResponseResponse, error) {
			return &pb.SubmitSurveyResponseResponse{Response: &pb.SurveyResponse{Id: "01J0M3RESP"}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub, "player": playerStub}}

	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m3", "--slug", "demo-flow-m3", "--admin-profile", "admin", "--player-profile", "player"}, rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitClientError, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	// The list-responses RPC itself succeeded (OK line emitted by
	// flowInvoke), but the flow's post-condition rejects the empty
	// responses array and writes a synthesised FAILED line — 11 total.
	if got, want := len(lines), 11; got != want {
		t.Fatalf("emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
	var last struct {
		Step   string `json:"step"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(lines[10], &last); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if last.Step != "list-responses" || last.Status != statusFailed {
		t.Errorf("got %q/%q, want list-responses/%s", last.Step, last.Status, statusFailed)
	}
}

func TestRunFlowGoldenM3_DryRun(t *testing.T) {
	rec := &flowFactoryRecorder{}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m3", "--slug", "demo-flow-m3", "--dry-run"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	if got, want := len(lines), 10; got != want {
		t.Fatalf("dry-run emitted %d lines, want %d", got, want)
	}
	if len(rec.requested) != 0 {
		t.Errorf("dry-run must not request profiles: %v", rec.requested)
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

func TestRunFlowGoldenM4_DryRunEmitsFourSteps(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rec := &flowFactoryRecorder{}
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m4", "--slug", "demo-window", "--dry-run"}, rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if len(rec.requested) != 0 {
		t.Errorf("dry-run should not request factories: %v", rec.requested)
	}
	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{"create-playtest", "await-auto-open", "await-auto-close", "assert-system-transitions"}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("dry-run emitted %d lines, want %d (stdout=%q)", got, want, stdout.String())
	}
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
		if got.Status != statusDryRun {
			t.Errorf("line %d status=%q, want DRY_RUN", i, got.Status)
		}
	}
}

func TestRunFlowGoldenM4_HappyPathStitchesStatusFlips(t *testing.T) {
	const createdID = "01J0M4PLAYTEST"
	getCalls := 0
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			if in.StartsAt == nil || in.EndsAt == nil {
				t.Errorf("create_request missing window: starts=%v ends=%v", in.StartsAt, in.EndsAt)
			}
			if in.DistributionModel != pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS {
				t.Errorf("distribution=%v want STEAM_KEYS", in.DistributionModel)
			}
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Status: pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT}}, nil
		},
		adminGetFunc: func(_ context.Context, _ *pb.AdminGetPlaytestRequest, _ ...grpc.CallOption) (*pb.AdminGetPlaytestResponse, error) {
			getCalls++
			// First poll → still DRAFT, second → OPEN, third → still OPEN, fourth → CLOSED.
			switch getCalls {
			case 1:
				return &pb.AdminGetPlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT}}, nil
			case 2:
				return &pb.AdminGetPlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
			case 3:
				return &pb.AdminGetPlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN}}, nil
			default:
				return &pb.AdminGetPlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED}}, nil
			}
		},
		listAuditLogFunc: func(_ context.Context, in *pb.ListAuditLogRequest, _ ...grpc.CallOption) (*pb.ListAuditLogResponse, error) {
			if in.ActorFilter != "system" || in.ActionFilter != "playtest.status_transition" {
				t.Errorf("audit filter=actor=%q action=%q want system/playtest.status_transition", in.ActorFilter, in.ActionFilter)
			}
			return &pb.ListAuditLogResponse{Entries: []*pb.AuditLogEntry{{Id: "a1"}, {Id: "a2"}}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub}}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m4", "--slug", "demo-window", "--admin-profile", "admin",
			"--start-offset", "1ms", "--end-offset", "2ms", "--poll-interval", "1ms",
			"--poll-timeout-open", "1s", "--poll-timeout-close", "1s"},
		rec.factory)
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)\nstdout=%s", code, exitOK, stderr.String(), stdout.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	wantSteps := []string{"create-playtest", "await-auto-open", "await-auto-close", "assert-system-transitions"}
	if got, want := len(lines), len(wantSteps); got != want {
		t.Fatalf("emitted %d lines, want %d", got, want)
	}
	for i, raw := range lines {
		var got struct {
			Step   string `json:"step"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("line %d: %v: %q", i, err, raw)
		}
		if got.Step != wantSteps[i] {
			t.Errorf("line %d step=%q want %q", i, got.Step, wantSteps[i])
		}
		if got.Status != statusOK {
			t.Errorf("line %d status=%q want OK", i, got.Status)
		}
	}
}

func TestRunFlowGoldenM4_TimeoutSurfacesFailedLine(t *testing.T) {
	const createdID = "01J0M4STUCK"
	adminStub := &stubPlaytestClient{
		createFunc: func(_ context.Context, in *pb.CreatePlaytestRequest, _ ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
			return &pb.CreatePlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Slug: in.Slug, Status: pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT}}, nil
		},
		adminGetFunc: func(_ context.Context, _ *pb.AdminGetPlaytestRequest, _ ...grpc.CallOption) (*pb.AdminGetPlaytestResponse, error) {
			return &pb.AdminGetPlaytestResponse{Playtest: &pb.Playtest{Id: createdID, Status: pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT}}, nil
		},
	}
	rec := &flowFactoryRecorder{byProfile: map[string]*stubPlaytestClient{"admin": adminStub}}
	var stdout, stderr bytes.Buffer
	code := runFlow(t.Context(), &stdout, &stderr, newFlowGlobals(),
		[]string{"golden-m4", "--slug", "demo-stuck", "--admin-profile", "admin",
			"--poll-interval", "1ms", "--poll-timeout-open", "10ms"},
		rec.factory)
	if code != exitClientError {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitClientError, stderr.String())
	}
	lines := splitNDJSON(stdout.Bytes())
	// create-playtest OK + await-auto-open FAILED = 2 lines total.
	if got := len(lines); got != 2 {
		t.Fatalf("emitted %d lines, want 2: %s", got, stdout.String())
	}
	var last struct {
		Step   string `json:"step"`
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(lines[1], &last); err != nil {
		t.Fatalf("line 1: %v", err)
	}
	if last.Step != "await-auto-open" || last.Status != statusFailed {
		t.Errorf("got step=%q status=%q, want await-auto-open/FAILED", last.Step, last.Status)
	}
	if last.Error.Code != "DeadlineExceeded" {
		t.Errorf("error code=%q, want DeadlineExceeded", last.Error.Code)
	}
}
