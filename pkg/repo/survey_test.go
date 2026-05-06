package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Sample survey question payload — opaque JSONB at the repo layer
// (typed marshalling is the service layer's concern). Mirrors the
// schema.md §"Survey entity spec" shape so the round-trip test pins
// the doc-level contract: question UUIDs, prompt strings, multi-choice
// option arrays, and a rating type all survive store → fetch.
func sampleSurveyQuestionsV1(t *testing.T) json.RawMessage {
	t.Helper()
	q := []map[string]any{
		{
			"id":       "11111111-1111-4111-8111-111111111111",
			"type":     "text",
			"prompt":   "What did you like?",
			"required": true,
		},
		{
			"id":       "22222222-2222-4222-8222-222222222222",
			"type":     "rating",
			"prompt":   "Overall fun?",
			"required": true,
		},
		{
			"id":       "33333333-3333-4333-8333-333333333333",
			"type":     "multi_choice",
			"prompt":   "Which platform?",
			"required": false,
			"options": []map[string]any{
				{"id": "opt-a", "label": "Steam"},
				{"id": "opt-b", "label": "Epic"},
			},
		},
	}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal sample questions: %v", err)
	}
	return b
}

func TestSurveyCreate_AssignsVersion1AndJSONBRoundTrip(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-create")
	store := repo.NewPgSurveyStore(testPool)
	ctx := context.Background()

	want := sampleSurveyQuestionsV1(t)
	got, err := store.Create(ctx, pt.ID, want)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID == uuid.Nil || got.CreatedAt.IsZero() {
		t.Errorf("Create did not populate id/created_at: %+v", got)
	}
	if got.Version != 1 {
		t.Errorf("first version = %d, want 1", got.Version)
	}
	if got.PlaytestID != pt.ID {
		t.Errorf("playtest id = %v, want %v", got.PlaytestID, pt.ID)
	}

	var wantDecoded, gotDecoded any
	if err := json.Unmarshal(want, &wantDecoded); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(got.Questions, &gotDecoded); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if !reflect.DeepEqual(gotDecoded, wantDecoded) {
		t.Errorf("questions round-trip mismatch:\n got  %#v\n want %#v", gotDecoded, wantDecoded)
	}
}

// PRD §4.7 / schema.md L154: a second Create on the same playtest is
// surfaced as a unique-constraint violation so the service layer can
// resolve it to AlreadyExists. The (playtest_id, version) UNIQUE
// constraint is the DB-level guard.
func TestSurveyCreate_DoubleCreateRejected(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-double")
	store := repo.NewPgSurveyStore(testPool)
	ctx := context.Background()

	if _, err := store.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t)); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := store.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if !errors.Is(err, repo.ErrUniqueViolation) {
		t.Fatalf("second Create returned %v, want ErrUniqueViolation", err)
	}
}

func TestSurveyEditAsNewVersion_BumpsAndPreservesPrior(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-edit")
	store := repo.NewPgSurveyStore(testPool)
	ctx := context.Background()

	v1, err := store.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	v2Questions, err := json.Marshal([]map[string]any{
		{
			"id":       "44444444-4444-4444-8444-444444444444",
			"type":     "text",
			"prompt":   "v2 question",
			"required": true,
		},
	})
	if err != nil {
		t.Fatalf("marshal v2: %v", err)
	}

	v2, err := store.EditAsNewVersion(ctx, pt.ID, v2Questions)
	if err != nil {
		t.Fatalf("EditAsNewVersion: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("v2.Version = %d, want 2", v2.Version)
	}
	if v2.ID == v1.ID {
		t.Errorf("v2 reused v1's id; want a new row")
	}

	current, err := store.GetCurrent(ctx, pt.ID)
	if err != nil {
		t.Fatalf("GetCurrent: %v", err)
	}
	if current.ID != v2.ID {
		t.Errorf("GetCurrent returned %v, want v2 %v", current.ID, v2.ID)
	}

	priorByID, err := store.GetByID(ctx, v1.ID)
	if err != nil {
		t.Fatalf("GetByID v1: %v", err)
	}
	if priorByID.Version != 1 {
		t.Errorf("v1 lookup version = %d, want 1", priorByID.Version)
	}
}

// EditAsNewVersion before any Create must surface ErrNotFound — the
// service layer translates that to errors.md NotFound rather than
// silently creating v1.
func TestSurveyEditAsNewVersion_NoPriorReturnsNotFound(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-edit-empty")
	store := repo.NewPgSurveyStore(testPool)

	_, err := store.EditAsNewVersion(context.Background(), pt.ID, sampleSurveyQuestionsV1(t))
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("EditAsNewVersion with no prior returned %v, want ErrNotFound", err)
	}
}

func TestSurveyGetCurrent_NotFoundOnEmptyPlaytest(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-empty")
	store := repo.NewPgSurveyStore(testPool)

	_, err := store.GetCurrent(context.Background(), pt.ID)
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("GetCurrent on empty playtest = %v, want ErrNotFound", err)
	}
}

// schema.md L152: (playtest_id, version) is unique. Two concurrent
// EditAsNewVersion calls cannot both produce the same version row.
// The advisory lock serialises them; even if it didn't, the index
// would surface a 23505. This test pins the DB-level guard.
func TestSurveyMigration_DuplicateVersionRejected(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-dup-version")
	ctx := context.Background()

	const insertSQL = `
		INSERT INTO survey (playtest_id, version, questions)
		VALUES ($1, $2, '[]'::jsonb)`

	if _, err := testPool.Exec(ctx, insertSQL, pt.ID, 1); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err := testPool.Exec(ctx, insertSQL, pt.ID, 1)
	if err == nil {
		t.Fatalf("duplicate version insert succeeded; want unique violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("got %v, want unique_violation (23505)", err)
	}
}

// PRD §5.6: one survey submission per (playtest_id, user_id), regardless
// of survey version bumps. The repo's SubmitOnce returns the existing
// row + true on a re-submit so the service layer maps to AlreadyExists.
func TestSurveyResponseSubmitOnce_IdempotentAcrossVersions(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-resp-once")
	surveyStore := repo.NewPgSurveyStore(testPool)
	respStore := repo.NewPgSurveyResponseStore(testPool)
	ctx := context.Background()

	v1, err := surveyStore.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	answers := json.RawMessage(`{"q1":"hello","q2":4}`)
	user := uuid.New()
	first, replay, err := respStore.SubmitOnce(ctx, &repo.SurveyResponse{
		PlaytestID: pt.ID,
		UserID:     user,
		SurveyID:   v1.ID,
		Answers:    answers,
	})
	if err != nil {
		t.Fatalf("first SubmitOnce: %v", err)
	}
	if replay {
		t.Errorf("first submit reported replay=true; want false")
	}
	if first.ID == uuid.Nil {
		t.Errorf("first submit returned zero id")
	}

	v2, err := surveyStore.EditAsNewVersion(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("EditAsNewVersion: %v", err)
	}

	// Re-submit against v2 — same (playtest_id, user_id), so the
	// UNIQUE conflict path returns the original v1 row, not a new
	// v2 row.
	again, replay, err := respStore.SubmitOnce(ctx, &repo.SurveyResponse{
		PlaytestID: pt.ID,
		UserID:     user,
		SurveyID:   v2.ID,
		Answers:    json.RawMessage(`{"q1":"changed"}`),
	})
	if err != nil {
		t.Fatalf("replay SubmitOnce: %v", err)
	}
	if !replay {
		t.Errorf("replay submit reported replay=false; want true")
	}
	if again.ID != first.ID {
		t.Errorf("replay returned id %v, want original %v", again.ID, first.ID)
	}
	if again.SurveyID != v1.ID {
		t.Errorf("replay surveyId = %v, want original v1 %v", again.SurveyID, v1.ID)
	}

	var gotAnswers map[string]any
	if err := json.Unmarshal(again.Answers, &gotAnswers); err != nil {
		t.Fatalf("unmarshal answers: %v", err)
	}
	wantAnswers := map[string]any{"q1": "hello", "q2": float64(4)}
	if !reflect.DeepEqual(gotAnswers, wantAnswers) {
		t.Errorf("replay answers mutated:\n got  %#v\n want %#v", gotAnswers, wantAnswers)
	}
}

func TestSurveyResponseListResponses_Pagination(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-list-page")
	surveyStore := repo.NewPgSurveyStore(testPool)
	respStore := repo.NewPgSurveyResponseStore(testPool)
	ctx := context.Background()

	v1, err := surveyStore.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := range 7 {
		_, _, err := respStore.SubmitOnce(ctx, &repo.SurveyResponse{
			PlaytestID: pt.ID,
			UserID:     uuid.New(),
			SurveyID:   v1.ID,
			Answers:    json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("seed SubmitOnce %d: %v", i, err)
		}
	}

	page, err := respStore.ListResponses(ctx, repo.SurveyResponsePageQuery{
		PlaytestID: pt.ID,
		Limit:      3,
	})
	if err != nil {
		t.Fatalf("ListResponses page1: %v", err)
	}
	if len(page.Rows) != 3 {
		t.Errorf("page1 len = %d, want 3", len(page.Rows))
	}
	if page.NextPageToken == "" {
		t.Errorf("page1 NextPageToken empty; want non-empty")
	}

	page2, err := respStore.ListResponses(ctx, repo.SurveyResponsePageQuery{
		PlaytestID: pt.ID,
		Limit:      3,
		PageToken:  page.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListResponses page2: %v", err)
	}
	if len(page2.Rows) != 3 {
		t.Errorf("page2 len = %d, want 3", len(page2.Rows))
	}
	if page2.NextPageToken == "" {
		t.Errorf("page2 NextPageToken empty; want non-empty (1 row remaining)")
	}

	page3, err := respStore.ListResponses(ctx, repo.SurveyResponsePageQuery{
		PlaytestID: pt.ID,
		Limit:      3,
		PageToken:  page2.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListResponses page3: %v", err)
	}
	if len(page3.Rows) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3.Rows))
	}
	if page3.NextPageToken != "" {
		t.Errorf("page3 NextPageToken = %q, want empty", page3.NextPageToken)
	}

	// Cursor ordering is (submitted_at, id) DESC — the IDs across
	// the three pages must not repeat.
	seen := map[uuid.UUID]bool{}
	for _, p := range []*repo.SurveyResponsePage{page, page2, page3} {
		for _, r := range p.Rows {
			if seen[r.ID] {
				t.Errorf("row %v appeared on multiple pages", r.ID)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != 7 {
		t.Errorf("union of pages = %d rows, want 7", len(seen))
	}
}

// PRD §5.6 cross-version aggregate split: ListResponses with a
// surveyId filter narrows to one version. Responses bound to other
// versions must not appear.
func TestSurveyResponseListResponses_FilterBySurvey(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-list-filter")
	surveyStore := repo.NewPgSurveyStore(testPool)
	respStore := repo.NewPgSurveyResponseStore(testPool)
	ctx := context.Background()

	v1, err := surveyStore.Create(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for i := range 3 {
		_, _, err := respStore.SubmitOnce(ctx, &repo.SurveyResponse{
			PlaytestID: pt.ID,
			UserID:     uuid.New(),
			SurveyID:   v1.ID,
			Answers:    json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("seed v1 %d: %v", i, err)
		}
	}

	v2, err := surveyStore.EditAsNewVersion(ctx, pt.ID, sampleSurveyQuestionsV1(t))
	if err != nil {
		t.Fatalf("EditAsNewVersion: %v", err)
	}
	for i := range 2 {
		_, _, err := respStore.SubmitOnce(ctx, &repo.SurveyResponse{
			PlaytestID: pt.ID,
			UserID:     uuid.New(),
			SurveyID:   v2.ID,
			Answers:    json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("seed v2 %d: %v", i, err)
		}
	}

	page, err := respStore.ListResponses(ctx, repo.SurveyResponsePageQuery{
		PlaytestID: pt.ID,
		SurveyID:   v2.ID,
	})
	if err != nil {
		t.Fatalf("ListResponses: %v", err)
	}
	if len(page.Rows) != 2 {
		t.Errorf("v2-only filter returned %d rows, want 2", len(page.Rows))
	}
	for _, r := range page.Rows {
		if r.SurveyID != v2.ID {
			t.Errorf("filtered row %v has surveyId %v, want %v", r.ID, r.SurveyID, v2.ID)
		}
	}
}

func TestSurveyResponseListResponses_RejectsBadToken(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "survey-bad-token")
	store := repo.NewPgSurveyResponseStore(testPool)

	_, err := store.ListResponses(context.Background(), repo.SurveyResponsePageQuery{
		PlaytestID: pt.ID,
		PageToken:  "not-a-real-token",
	})
	if !errors.Is(err, repo.ErrInvalidSurveyResponseToken) {
		t.Errorf("ListResponses with bad token = %v, want ErrInvalidSurveyResponseToken", err)
	}
}
