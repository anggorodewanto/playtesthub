package repo

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Audit-action constants for the M5 set. Doc-of-truth: schema.md
// §"AuditLog — `action` enum".
const (
	ActionApplicantAutoApproved = "applicant.auto_approved"
)

// AppendApplicantAutoApproved records a successful auto-approve via the
// PRD §5.4 / M5.A signup chain. System-emitted (no actor — the
// promotion is initiated by the player's own Signup, but the audit row
// tracks the system action, distinct from manual ApproveApplicant
// clicks which carry the admin actor on `applicant.approve`). The
// distinct action enables audit-log filters to separate manual vs
// auto attribution. codeID is the granted code id for STEAM_KEYS /
// AGS_CAMPAIGN playtests; pass uuid.Nil to omit the field (reserved
// for future distribution models that do not allocate from a pool).
func AppendApplicantAutoApproved(ctx context.Context, store AuditLogStore, namespace string, playtestID, applicantID, codeID uuid.UUID, autoApprovedAt time.Time) error {
	payload := map[string]any{
		"applicantId":    applicantID.String(),
		"autoApprovedAt": autoApprovedAt.UTC().Format(time.RFC3339Nano),
	}
	if codeID != uuid.Nil {
		payload["codeId"] = codeID.String()
	}
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionApplicantAutoApproved, payload)
}
