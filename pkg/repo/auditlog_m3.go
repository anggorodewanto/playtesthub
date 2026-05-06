package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Audit-action constants for the M3 set. Doc-of-truth: schema.md
// §"AuditLog — `action` enum". The action column carries no DB CHECK,
// so consistency between code, docs, and persisted rows is enforced by
// the typed writers below.
const (
	ActionSurveyCreate = "survey.create"
	ActionSurveyEdit   = "survey.edit"
)

// AppendSurveyCreate records a first-version survey insert. System-
// emitted (no admin actor — the create was initiated by an admin RPC
// but the audit row tracks the surveyId mint, not the admin click).
// Payload per schema.md §"AuditLog — `action` enum": questionCount.
func AppendSurveyCreate(ctx context.Context, store AuditLogStore, namespace string, playtestID, surveyID uuid.UUID, questionCount int) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionSurveyCreate, map[string]any{
		"surveyId":      surveyID.String(),
		"questionCount": questionCount,
	})
}

// AppendSurveyEdit records a version-bumping edit. Per schema.md L59
// the row carries the **full before/after question set**: survey
// questions are not secret, and the full diff is the accountability
// mechanism for survey changes. Both payloads are the JSONB question
// arrays as persisted on the Survey row.
func AppendSurveyEdit(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor, beforeSurveyID, afterSurveyID uuid.UUID, beforeQuestions, afterQuestions json.RawMessage) error {
	beforeBytes, err := json.Marshal(map[string]any{
		"surveyId":  beforeSurveyID.String(),
		"questions": json.RawMessage(beforeQuestions),
	})
	if err != nil {
		return fmt.Errorf("marshalling survey.edit before: %w", err)
	}
	afterBytes, err := json.Marshal(map[string]any{
		"surveyId":  afterSurveyID.String(),
		"questions": json.RawMessage(afterQuestions),
	})
	if err != nil {
		return fmt.Errorf("marshalling survey.edit after: %w", err)
	}
	if _, err := store.Append(ctx, &AuditLog{
		Namespace:   namespace,
		PlaytestID:  &playtestID,
		ActorUserID: &actor,
		Action:      ActionSurveyEdit,
		Before:      beforeBytes,
		After:       afterBytes,
	}); err != nil {
		return fmt.Errorf("appending survey.edit audit row: %w", err)
	}
	return nil
}
