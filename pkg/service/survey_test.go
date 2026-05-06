package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeSurveyStore is an in-memory SurveyStore. It mirrors the
// production constraints exercised by the M3 phase 3 handlers — Create
// is natural-key on playtest_id (returns ErrUniqueViolation on a
// second insert), EditAsNewVersion bumps version atomically, GetByID
// resolves a survey row by primary key.
type fakeSurveyStore struct {
	rows []*repo.Survey
}

func (f *fakeSurveyStore) Create(_ context.Context, playtestID uuid.UUID, questions json.RawMessage) (*repo.Survey, error) {
	for _, r := range f.rows {
		if r.PlaytestID == playtestID && r.Version == 1 {
			return nil, repo.ErrUniqueViolation
		}
	}
	row := &repo.Survey{
		ID:         uuid.New(),
		PlaytestID: playtestID,
		Version:    1,
		Questions:  append(json.RawMessage{}, questions...),
		CreatedAt:  time.Now(),
	}
	f.rows = append(f.rows, row)
	clone := *row
	return &clone, nil
}

func (f *fakeSurveyStore) EditAsNewVersion(_ context.Context, playtestID uuid.UUID, questions json.RawMessage) (*repo.Survey, error) {
	highest := 0
	for _, r := range f.rows {
		if r.PlaytestID == playtestID && r.Version > highest {
			highest = r.Version
		}
	}
	if highest == 0 {
		return nil, repo.ErrNotFound
	}
	row := &repo.Survey{
		ID:         uuid.New(),
		PlaytestID: playtestID,
		Version:    highest + 1,
		Questions:  append(json.RawMessage{}, questions...),
		CreatedAt:  time.Now(),
	}
	f.rows = append(f.rows, row)
	clone := *row
	return &clone, nil
}

func (f *fakeSurveyStore) GetCurrent(_ context.Context, playtestID uuid.UUID) (*repo.Survey, error) {
	var current *repo.Survey
	for _, r := range f.rows {
		if r.PlaytestID != playtestID {
			continue
		}
		if current == nil || r.Version > current.Version {
			current = r
		}
	}
	if current == nil {
		return nil, repo.ErrNotFound
	}
	clone := *current
	return &clone, nil
}

func (f *fakeSurveyStore) GetByID(_ context.Context, surveyID uuid.UUID) (*repo.Survey, error) {
	for _, r := range f.rows {
		if r.ID == surveyID {
			clone := *r
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

// surveyTestRig bundles the test server with every fake store the
// CreateSurvey / EditSurvey / GetSurvey handlers exercise.
type surveyTestRig struct {
	svr        *PlaytesthubServiceServer
	playtests  *fakePlaytestStore
	applicants *fakeApplicantStore
	surveys    *fakeSurveyStore
	audit      *fakeAuditLogStore
}

func withSurveyStores(t *testing.T) surveyTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	surveys := &fakeSurveyStore{}
	audit := &fakeAuditLogStore{}
	svr = svr.WithSurveyStore(surveys).WithAuditLogStore(audit)
	return surveyTestRig{svr: svr, playtests: pt, applicants: ap, surveys: surveys, audit: audit}
}

func textQ(prompt string, required bool) *pb.SurveyQuestion {
	return &pb.SurveyQuestion{
		Type:     pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT,
		Prompt:   prompt,
		Required: required,
	}
}

func ratingQ(prompt string) *pb.SurveyQuestion {
	return &pb.SurveyQuestion{
		Type:   pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING,
		Prompt: prompt,
	}
}

func multiQ(prompt string, allowMultiple bool, labels ...string) *pb.SurveyQuestion {
	q := &pb.SurveyQuestion{
		Type:          pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_MULTI_CHOICE,
		Prompt:        prompt,
		AllowMultiple: allowMultiple,
	}
	for _, l := range labels {
		q.Options = append(q.Options, &pb.MultiChoiceOption{Label: l})
	}
	return q
}

// ---------------- CreateSurvey ----------------------------------------------

func TestCreateSurvey_HappyPath_MintsIDsAndPointsPlaytest(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("survey-game")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	resp, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions: []*pb.SurveyQuestion{
			textQ("How was the matchmaking?", true),
			multiQ("Which platforms did you play on?", true, "Steam", "Xbox", "PSN"),
		},
	})
	if err != nil {
		t.Fatalf("CreateSurvey: %v", err)
	}
	survey := resp.GetSurvey()
	if survey.GetVersion() != 1 {
		t.Errorf("version = %d, want 1", survey.GetVersion())
	}
	if got := len(survey.GetQuestions()); got != 2 {
		t.Fatalf("questions = %d, want 2", got)
	}
	for i, q := range survey.GetQuestions() {
		if q.GetId() == "" {
			t.Errorf("question[%d].id empty (server should mint)", i)
		}
	}
	multi := survey.GetQuestions()[1]
	if got := len(multi.GetOptions()); got != 3 {
		t.Fatalf("multi-choice options = %d, want 3", got)
	}
	for i, opt := range multi.GetOptions() {
		if opt.GetId() == "" {
			t.Errorf("option[%d].id empty (server should mint)", i)
		}
	}

	// Playtest pointer flipped.
	if rig.playtests.rows[0].SurveyID == nil || rig.playtests.rows[0].SurveyID.String() != survey.GetId() {
		t.Errorf("playtest.survey_id not pointing at created survey: got %v want %s", rig.playtests.rows[0].SurveyID, survey.GetId())
	}
	// Audit row.
	if got := len(rig.audit.rows); got != 1 || rig.audit.rows[0].Action != repo.ActionSurveyCreate {
		t.Fatalf("audit rows = %d, want 1 survey.create; got %v", got, auditActions(rig.audit.rows))
	}
	after := mustJSON(t, rig.audit.rows[0].After)
	if got := after["questionCount"]; got != float64(2) {
		t.Errorf("survey.create after.questionCount = %v, want 2", got)
	}
}

func TestCreateSurvey_DiscardsClientSuppliedIDs(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("ignored-ids")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	clientID := "client-supplied-id-should-be-ignored"
	q := textQ("prompt", false)
	q.Id = clientID

	resp, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{q},
	})
	if err != nil {
		t.Fatalf("CreateSurvey: %v", err)
	}
	got := resp.GetSurvey().GetQuestions()[0].GetId()
	if got == clientID || got == "" {
		t.Errorf("server should mint a fresh UUID; got %q", got)
	}
}

func TestCreateSurvey_SecondCall_AlreadyExists(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("twice")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	if _, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("once", false)},
	}); err != nil {
		t.Fatalf("first CreateSurvey: %v", err)
	}
	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("again", false)},
	})
	requireStatus(t, err, codes.AlreadyExists)
}

func TestCreateSurvey_RejectsEmptyQuestions(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("empty-q")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{},
	})
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreateSurvey_RejectsTooManyQuestions(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("too-many")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	qs := make([]*pb.SurveyQuestion, 0, maxSurveyQuestions+1)
	for i := 0; i < maxSurveyQuestions+1; i++ {
		qs = append(qs, textQ("q", false))
	}
	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  qs,
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "50-question cap")
}

func TestCreateSurvey_RejectsOverlongPrompt(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("long-prompt")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	long := strings.Repeat("x", maxSurveyPromptChars+1)
	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ(long, false)},
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "1000-char cap")
}

func TestCreateSurvey_RejectsTooFewMultiChoiceOptions(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("few-opts")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{multiQ("pick", false, "only-one")},
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "2–20 bound")
}

func TestCreateSurvey_RejectsTextQuestionWithOptions(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("bad-text")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	q := textQ("prompt", false)
	q.Options = []*pb.MultiChoiceOption{{Label: "nope"}}
	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{q},
	})
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreateSurvey_PlaytestNotFound(t *testing.T) {
	rig := withSurveyStores(t)
	_, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		Questions:  []*pb.SurveyQuestion{textQ("prompt", false)},
	})
	requireStatus(t, err, codes.NotFound)
}

func TestCreateSurvey_Unauthenticated(t *testing.T) {
	rig := withSurveyStores(t)
	_, err := rig.svr.CreateSurvey(context.Background(), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		Questions:  []*pb.SurveyQuestion{textQ("prompt", false)},
	})
	requireStatus(t, err, codes.Unauthenticated)
}

// ---------------- EditSurvey ------------------------------------------------

func TestEditSurvey_PreservesIDs_MintsNewOnes_BumpsVersion(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("edit")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	created, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions: []*pb.SurveyQuestion{
			textQ("kept", true),
			multiQ("pick one", false, "A", "B"),
		},
	})
	if err != nil {
		t.Fatalf("seed CreateSurvey: %v", err)
	}
	keptQID := created.GetSurvey().GetQuestions()[0].GetId()
	keptOptID := created.GetSurvey().GetQuestions()[1].GetOptions()[0].GetId()

	resp, err := rig.svr.EditSurvey(authCtx(uuid.New()), &pb.EditSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions: []*pb.SurveyQuestion{
			{
				Id:       keptQID,
				Type:     pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT,
				Prompt:   "kept (renamed)",
				Required: false,
			},
			{
				// New multi-choice with one preserved option id + one fresh.
				Type:   pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_MULTI_CHOICE,
				Prompt: "fresh question, partially preserved options",
				Options: []*pb.MultiChoiceOption{
					{Id: keptOptID, Label: "A renamed"}, // preserved
					{Label: "C"},                        // fresh
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("EditSurvey: %v", err)
	}
	got := resp.GetSurvey()
	if got.GetVersion() != 2 {
		t.Errorf("version = %d, want 2", got.GetVersion())
	}
	if got.GetQuestions()[0].GetId() != keptQID {
		t.Errorf("question[0].id = %q, want %q (preserved)", got.GetQuestions()[0].GetId(), keptQID)
	}
	if got.GetQuestions()[1].GetId() == "" {
		t.Errorf("question[1].id empty (server should mint a fresh UUID)")
	}
	// Even though the new question got a fresh question id, the option
	// id we passed in should be preserved (orphaned per-option id
	// preservation).
	if got.GetQuestions()[1].GetOptions()[0].GetId() != keptOptID {
		t.Errorf("option[0].id = %q, want %q (preserved)", got.GetQuestions()[1].GetOptions()[0].GetId(), keptOptID)
	}
	if got.GetQuestions()[1].GetOptions()[1].GetId() == "" {
		t.Errorf("option[1].id empty (server should mint)")
	}
	// playtest.survey_id repointed.
	if rig.playtests.rows[0].SurveyID == nil || rig.playtests.rows[0].SurveyID.String() != got.GetId() {
		t.Errorf("playtest.survey_id not repointed to new version")
	}
	// Audit: one survey.create + one survey.edit.
	if len(rig.audit.rows) != 2 {
		t.Fatalf("audit rows = %d, want 2 (survey.create + survey.edit)", len(rig.audit.rows))
	}
	editRow := rig.audit.rows[1]
	if editRow.Action != repo.ActionSurveyEdit {
		t.Fatalf("audit[1].action = %q, want survey.edit", editRow.Action)
	}
	before := mustJSON(t, editRow.Before)
	after := mustJSON(t, editRow.After)
	if before["surveyId"] == nil || after["surveyId"] == nil {
		t.Errorf("survey.edit row missing before/after surveyId: before=%v after=%v", before, after)
	}
	if before["questions"] == nil || after["questions"] == nil {
		t.Errorf("survey.edit row missing before/after questions diff")
	}
}

func TestEditSurvey_NoExistingSurvey_FailedPrecondition(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("none")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.EditSurvey(authCtx(uuid.New()), &pb.EditSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("oops", false)},
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

func TestEditSurvey_UnknownQuestionID_InvalidArgument(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("unknown")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	if _, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("seed", false)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	bogus := &pb.SurveyQuestion{
		Id:     uuid.NewString(),
		Type:   pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT,
		Prompt: "bogus",
	}
	_, err := rig.svr.EditSurvey(authCtx(uuid.New()), &pb.EditSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{bogus},
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "does not match")
}

func TestEditSurvey_DuplicateQuestionID_InvalidArgument(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("dup")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	created, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("a", false)},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	keptID := created.GetSurvey().GetQuestions()[0].GetId()
	_, err = rig.svr.EditSurvey(authCtx(uuid.New()), &pb.EditSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions: []*pb.SurveyQuestion{
			{Id: keptID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT, Prompt: "first"},
			{Id: keptID, Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT, Prompt: "second-dup"},
		},
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "duplicated")
}

// ---------------- GetSurvey -------------------------------------------------

func TestGetSurvey_HappyPath(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("get")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	if _, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("hello", false)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp, err := rig.svr.GetSurvey(authCtx(uuid.New()), &pb.GetSurveyRequest{
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("GetSurvey: %v", err)
	}
	if got := resp.GetSurvey().GetVersion(); got != 1 {
		t.Errorf("version = %d, want 1", got)
	}
	if got := len(resp.GetSurvey().GetQuestions()); got != 1 {
		t.Errorf("questions = %d, want 1", got)
	}
}

func TestGetSurvey_NoSurveyConfigured_NotFound(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("nosurvey")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.GetSurvey(authCtx(uuid.New()), &pb.GetSurveyRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetSurvey_DraftPlaytest_NotFound(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("draft")
	pt.Status = statusDraft
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.GetSurvey(authCtx(uuid.New()), &pb.GetSurveyRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetSurvey_ClosedPlaytest_NonApprovedApplicant_NotFound(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("closed")
	pt.Status = statusClosed
	rig.playtests.rows = append(rig.playtests.rows, pt)
	if _, err := rig.svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("q", false)},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Need to repoint after CreateSurvey because openPlaytest sets
	// status after; flip back to OPEN long enough for create then to
	// CLOSED for the assertion path. Easier: just leave closed and let
	// Create fail? Create only checks DeletedAt + namespace; doesn't
	// gate on status. The seed above succeeded.

	_, err := rig.svr.GetSurvey(authCtx(uuid.New()), &pb.GetSurveyRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetSurvey_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withSurveyStores(t)
	pt := openPlaytest("gone")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.GetSurvey(authCtx(uuid.New()), &pb.GetSurveyRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetSurvey_Unauthenticated(t *testing.T) {
	rig := withSurveyStores(t)
	_, err := rig.svr.GetSurvey(context.Background(), &pb.GetSurveyRequest{
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

// ---------------- Surface guard: handlers compile when store is unwired ----

func TestCreateSurvey_NoSurveyStore_Internal(t *testing.T) {
	svr, pt, _ := newTestServer()
	row := openPlaytest("nostore")
	pt.rows = append(pt.rows, row)

	_, err := svr.CreateSurvey(authCtx(uuid.New()), &pb.CreateSurveyRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		Questions:  []*pb.SurveyQuestion{textQ("hi", false)},
	})
	requireStatus(t, err, codes.Internal)
}
