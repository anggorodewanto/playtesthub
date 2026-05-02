package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Audit-action constants for the M2 set. Doc-of-truth: schema.md
// §"AuditLog — `action` enum". The action column carries no DB CHECK,
// so consistency between code, docs, and persisted rows is enforced by
// the typed writers below + migration 0002's round-trip test.
const (
	ActionNDAAccept                   = "nda.accept"
	ActionApplicantApprove            = "applicant.approve"
	ActionApplicantReject             = "applicant.reject"
	ActionCodeUpload                  = "code.upload"
	ActionCodeUploadRejected          = "code.upload_rejected"
	ActionCodeGrantOrphaned           = "code.grant_orphaned"
	ActionApplicantDMSent             = "applicant.dm_sent"
	ActionApplicantDMFailed           = "applicant.dm_failed"
	ActionDMCircuitOpened             = "dm.circuit_opened"
	ActionDMCircuitClosed             = "dm.circuit_closed"
	ActionCampaignCreate              = "campaign.create"
	ActionCampaignCreateFailed        = "campaign.create_failed"
	ActionCampaignGenerateCodes       = "campaign.generate_codes"
	ActionCampaignGenerateCodesFailed = "campaign.generate_codes_failed"
)

// Each writer marshals the schema.md payload for a single action and
// calls AuditLogStore.Append. They return errors only — call sites
// rarely need the persisted row back. System-emitted events (every
// row marked **System-emitted** in schema.md) take no actor argument
// so the field cannot be set by mistake.

// AppendNDAAccept records a click-accept on the NDA. Admin-attributed
// when an admin retro-accepts on behalf of a player; player-attributed
// (actor = the player's userID) on the normal flow per PRD §4.7.
func AppendNDAAccept(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, actor *uuid.UUID, applicantID uuid.UUID, ndaVersionHash string) error {
	return appendAction(ctx, store, namespace, &playtestID, actor, ActionNDAAccept, map[string]any{
		"applicantId":    applicantID.String(),
		"ndaVersionHash": ndaVersionHash,
	})
}

// AppendApplicantApprove records a successful PENDING → APPROVED
// transition. The raw code value is **never** included (PRD §6 / docs/
// schema.md redaction rule); only the grantedCodeId reference.
func AppendApplicantApprove(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor, applicantID, grantedCodeID uuid.UUID) error {
	return appendAction(ctx, store, namespace, &playtestID, &actor, ActionApplicantApprove, map[string]any{
		"applicantId":   applicantID.String(),
		"grantedCodeId": grantedCodeID.String(),
	})
}

// AppendApplicantReject records the terminal PENDING → REJECTED
// transition. rejectionReason may be empty.
func AppendApplicantReject(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor, applicantID uuid.UUID, rejectionReason string) error {
	return appendAction(ctx, store, namespace, &playtestID, &actor, ActionApplicantReject, map[string]any{
		"applicantId":     applicantID.String(),
		"rejectionReason": rejectionReason,
	})
}

// AppendCodeUpload records a STEAM_KEYS CSV batch ingest. **Raw code
// values are never persisted** (schema.md L51); the row carries only
// the count + sha256 + filename.
func AppendCodeUpload(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor uuid.UUID, count int, sha256Hex, filename string) error {
	return appendAction(ctx, store, namespace, &playtestID, &actor, ActionCodeUpload, map[string]any{
		"count":    count,
		"sha256":   sha256Hex,
		"filename": filename,
	})
}

// AppendCodeUploadRejected records a whole-file CSV rejection (PRD
// §4.3). reason is a short machine-readable tag (e.g. "size_exceeded",
// "charset_violation"); rowCount is rows-parsed-before-rejection.
// System-emitted: no actor.
func AppendCodeUploadRejected(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, filename, reason string, rowCount int) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCodeUploadRejected, map[string]any{
		"filename": filename,
		"reason":   reason,
		"rowCount": rowCount,
	})
}

// AppendCodeGrantOrphaned records the fenced-finalize-affected-0-rows
// case (PRD §4.1 step 6b). System-emitted; no actor.
func AppendCodeGrantOrphaned(ctx context.Context, store AuditLogStore, namespace string, playtestID, applicantID, codeID, userID uuid.UUID, originalReservedAt time.Time) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCodeGrantOrphaned, map[string]any{
		"applicantId":        applicantID.String(),
		"codeId":             codeID.String(),
		"userId":             userID.String(),
		"originalReservedAt": originalReservedAt.UTC().Format(time.RFC3339Nano),
	})
}

// AppendApplicantDMSent is written **only** on a successful manual
// Retry DM (PRD §5.4). actor = the admin who clicked Retry.
func AppendApplicantDMSent(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor, applicantID uuid.UUID, discordUserID string) error {
	return appendAction(ctx, store, namespace, &playtestID, &actor, ActionApplicantDMSent, map[string]any{
		"applicantId":   applicantID.String(),
		"discordUserId": discordUserID,
	})
}

// AppendApplicantDMFailed records a failed DM attempt. error is
// truncated to 500 bytes preserving UTF-8 codepoint boundaries before
// writing (matches the applicant.last_dm_error 500-byte rule).
// System-emitted; no actor.
func AppendApplicantDMFailed(ctx context.Context, store AuditLogStore, namespace string, playtestID, applicantID uuid.UUID, errMsg string, attemptAt time.Time) error {
	truncated := truncateUTF8String(errMsg, applicantLastDMErrorMaxBytes)
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionApplicantDMFailed, map[string]any{
		"applicantId": applicantID.String(),
		"error":       truncated,
		"attemptAt":   attemptAt.UTC().Format(time.RFC3339Nano),
	})
}

// AppendDMCircuitOpened / AppendDMCircuitClosed bracket a DM circuit-
// breaker open period. Both system-emitted; namespace-scoped (no
// playtestId since the circuit is global per docs/dm-queue.md).
func AppendDMCircuitOpened(ctx context.Context, store AuditLogStore, namespace string, trippedAt time.Time, recentFailureCount int) error {
	return appendAction(ctx, store, namespace, nil, nil, ActionDMCircuitOpened, map[string]any{
		"trippedAt":          trippedAt.UTC().Format(time.RFC3339Nano),
		"recentFailureCount": recentFailureCount,
	})
}

func AppendDMCircuitClosed(ctx context.Context, store AuditLogStore, namespace string, closedAt time.Time) error {
	return appendAction(ctx, store, namespace, nil, nil, ActionDMCircuitClosed, map[string]any{
		"closedAt": closedAt.UTC().Format(time.RFC3339Nano),
	})
}

// AppendCampaignCreate records a successful AGS Item + Campaign
// provision. System-emitted.
func AppendCampaignCreate(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, agsItemID, agsCampaignID, itemName string, initialCodeQuantity int) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCampaignCreate, map[string]any{
		"agsItemId":           agsItemID,
		"agsCampaignId":       agsCampaignID,
		"itemName":            itemName,
		"initialCodeQuantity": initialCodeQuantity,
	})
}

// AppendCampaignCreateFailed records an Item/Campaign provisioning
// failure including the cleanup-matrix outcome (docs/ags-failure-modes
// .md). System-emitted.
func AppendCampaignCreateFailed(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, errMsg string, cleanupAttempted, cleanupSuccess bool) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCampaignCreateFailed, map[string]any{
		"error":            errMsg,
		"cleanupAttempted": cleanupAttempted,
		"cleanupSuccess":   cleanupSuccess,
	})
}

// AppendCampaignGenerateCodes covers both initial-generate at
// playtest creation and TopUpCodes (schema.md L57). System-emitted.
func AppendCampaignGenerateCodes(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, agsCampaignID string, quantity, totalPoolSize int) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCampaignGenerateCodes, map[string]any{
		"agsCampaignId": agsCampaignID,
		"quantity":      quantity,
		"totalPoolSize": totalPoolSize,
	})
}

// AppendCampaignGenerateCodesFailed covers both initial-generate and
// TopUpCodes failures (schema.md L58). System-emitted.
func AppendCampaignGenerateCodesFailed(ctx context.Context, store AuditLogStore, namespace string, playtestID uuid.UUID, agsCampaignID string, requestedQuantity int, errMsg string) error {
	return appendAction(ctx, store, namespace, &playtestID, nil, ActionCampaignGenerateCodesFailed, map[string]any{
		"agsCampaignId":     agsCampaignID,
		"requestedQuantity": requestedQuantity,
		"error":             errMsg,
	})
}

// appendAction is the shared marshal-and-Append path. Every M2 writer
// records a single "after" payload; "before" stays empty (default {})
// because these events are point-in-time records, not state diffs.
func appendAction(ctx context.Context, store AuditLogStore, namespace string, playtestID, actor *uuid.UUID, action string, after map[string]any) error {
	afterBytes, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("marshalling %s payload: %w", action, err)
	}
	if _, err := store.Append(ctx, &AuditLog{
		Namespace:   namespace,
		PlaytestID:  playtestID,
		ActorUserID: actor,
		Action:      action,
		After:       afterBytes,
	}); err != nil {
		return fmt.Errorf("appending %s audit row: %w", action, err)
	}
	return nil
}

// truncateUTF8String is the string-keyed twin of truncateUTF8 used by
// the DM-failed audit writer.
func truncateUTF8String(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
