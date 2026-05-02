package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Audit-action constants for the M1 admin write path. Doc-of-truth:
// schema.md §"AuditLog — `action` enum". The action column carries no
// DB CHECK; the typed writers below pin the action strings in code.
const (
	ActionPlaytestEdit             = "playtest.edit"
	ActionNDAEdit                  = "nda.edit"
	ActionPlaytestSoftDelete       = "playtest.soft_delete"
	ActionPlaytestStatusTransition = "playtest.status_transition"
)

// AppendNDAEdit records an NDA-text mutation on a Playtest. Per
// schema.md L42 the row is the **only** place where the full NDA text
// is intentionally persisted to the audit log — every other audited
// action redacts free-text content. before/after carry the literal
// strings; if a side is empty (initial set, NDA wipe) the matching
// payload is still emitted with an empty string so consumers can
// diff without nil-checks.
func AppendNDAEdit(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor uuid.UUID, beforeText, afterText string) error {
	beforeBytes, err := json.Marshal(map[string]any{"ndaText": beforeText})
	if err != nil {
		return fmt.Errorf("marshalling nda.edit before: %w", err)
	}
	afterBytes, err := json.Marshal(map[string]any{"ndaText": afterText})
	if err != nil {
		return fmt.Errorf("marshalling nda.edit after: %w", err)
	}
	if _, err := store.Append(ctx, &AuditLog{
		Namespace:   namespace,
		PlaytestID:  &playtestID,
		ActorUserID: &actor,
		Action:      ActionNDAEdit,
		Before:      beforeBytes,
		After:       afterBytes,
	}); err != nil {
		return fmt.Errorf("appending nda.edit audit row: %w", err)
	}
	return nil
}
