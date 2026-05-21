package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
)

// Audit-action constants for the M5 set. Doc-of-truth: schema.md
// §"AuditLog — `action` enum".
const (
	ActionApplicantAutoApproved = "applicant.auto_approved"
	ActionADTLinkageCreate      = "adt_linkage.create"
	ActionADTLinkageDelete      = "adt_linkage.delete"
	ActionADTLinkageRecover     = "adt_linkage.recover"
	ActionAnnouncementCreate    = "announcement.create"
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

// AppendADTLinkageCreate records the studio ↔ ADT-namespace identity
// row inserted by CompleteADTLink (PRD §4.8.2). Admin-attributed:
// actorUserID = the admin who started the link (recovered from the
// adt_link_pending row at commit time per schema.md). The audit row
// is namespace-scoped on the playtesthub side (AGS_NAMESPACE) but
// playtestID is nil — linkages are not bound to any single playtest
// row by design (D1 — per-studio linkage).
func AppendADTLinkageCreate(ctx context.Context, store AuditLogStore, namespace string, actor uuid.UUID, linkageID uuid.UUID, studioNamespace, adtNamespace string) error {
	return appendAction(ctx, store, namespace, nil, &actor, ActionADTLinkageCreate, map[string]any{
		"adtLinkageId":    linkageID.String(),
		"studioNamespace": studioNamespace,
		"adtNamespace":    adtNamespace,
		"linkedBy":        agsid.Format(actor),
	})
}

// AppendAnnouncementCreate records a successful CreateAnnouncement
// (PRD §5.4 "Bulk announcements"). Admin-attributed. **subject + message
// are NEVER in the audit JSONB** — only IDs + counts. PII recovery for
// the body lives on the announcement table itself.
func AppendAnnouncementCreate(ctx context.Context, store AuditLogStore, namespace string, actor uuid.UUID, announcementID, playtestID uuid.UUID, sendToFilter string, recipientCount int32) error {
	return appendAction(ctx, store, namespace, &playtestID, &actor, ActionAnnouncementCreate, map[string]any{
		"announcementId": announcementID.String(),
		"playtestId":     playtestID.String(),
		"sendToFilter":   sendToFilter,
		"recipientCount": recipientCount,
		"createdBy":      agsid.Format(actor),
	})
}

// AppendADTLinkageDelete records the soft-delete of an adt_linkage row
// by UnlinkADT (PRD §4.8). Admin-attributed. Idempotent: callers SHOULD
// only invoke this when the underlying SoftDelete affected a row, not
// on repeat unlinks against an already-deleted linkage (those are
// no-ops per schema.md).
func AppendADTLinkageDelete(ctx context.Context, store AuditLogStore, namespace string, actor uuid.UUID, linkageID uuid.UUID, studioNamespace, adtNamespace string) error {
	return appendAction(ctx, store, namespace, nil, &actor, ActionADTLinkageDelete, map[string]any{
		"adtLinkageId":    linkageID.String(),
		"studioNamespace": studioNamespace,
		"adtNamespace":    adtNamespace,
	})
}

// AppendADTLinkageRecover records the adoption of an orphan ADT-side
// linkage flag via RecoverADTLinkage (PRD §4.8). Admin-attributed:
// actorUserID = the admin who called the recovery RPC. Payload mirrors
// adt_linkage.create — the only material difference is the action
// string (an audit-log filter on action separates orphan-recovery from
// the regular create flow).
func AppendADTLinkageRecover(ctx context.Context, store AuditLogStore, namespace string, actor uuid.UUID, linkageID uuid.UUID, studioNamespace, adtNamespace string) error {
	return appendAction(ctx, store, namespace, nil, &actor, ActionADTLinkageRecover, map[string]any{
		"adtLinkageId":    linkageID.String(),
		"studioNamespace": studioNamespace,
		"adtNamespace":    adtNamespace,
		"linkedBy":        agsid.Format(actor),
	})
}
