package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// auditActorSystem is the wire-level sentinel a client sends in
// ListAuditLogRequest.actor_filter to narrow to system-emitted rows
// (actor_user_id IS NULL). Lower-case to keep the surface forgiving —
// the proto comment cites it as 'system' and PRD §4.7 / §5.7 do too.
const auditActorSystem = "system"

// ListAuditLog returns one page of audit_log rows for the playtest,
// ordered (created_at, id) DESC, DESC. Admin endpoint — requires actor +
// namespace match. Soft-deleted playtests are NotFound (mirrors
// ListSurveyResponses); the page itself ignores soft-delete state of
// the playtest because audit history outlives the playtest row.
//
// Filters compose: actor_filter ('system' or a UUID) + action_filter
// (exact match) + cursor narrow the stream. Malformed page_token →
// InvalidArgument. Malformed actor_filter (non-'system', non-UUID) →
// InvalidArgument.
func (s *PlaytesthubServiceServer) ListAuditLog(ctx context.Context, req *pb.ListAuditLogRequest) (*pb.ListAuditLogResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.audit == nil {
		return nil, status.Error(codes.Internal, "audit log store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	q := repo.AuditLogPageQuery{
		PlaytestID:   playtestID,
		ActionFilter: req.GetActionFilter(),
		PageToken:    req.GetPageToken(),
		Limit:        int(req.GetPageSize()),
	}
	if raw := req.GetActorFilter(); raw != "" {
		if raw == auditActorSystem {
			q.ActorFilter = auditActorSystem
		} else {
			parsed, parseErr := uuid.Parse(raw)
			if parseErr != nil {
				return nil, status.Errorf(codes.InvalidArgument, "actor_filter %q must be 'system' or a uuid: %v", raw, parseErr)
			}
			q.ActorUserID = &parsed
		}
	}

	page, err := s.audit.List(ctx, q)
	if errors.Is(err, repo.ErrInvalidAuditLogToken) {
		return nil, status.Error(codes.InvalidArgument, "page_token is malformed")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing audit log: %v", err)
	}

	out := make([]*pb.AuditLogEntry, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, auditLogToProto(r))
	}
	return &pb.ListAuditLogResponse{Entries: out, NextPageToken: page.NextPageToken}, nil
}

// auditLogToProto renders a stored audit row as the wire message. The
// before / after JSONB columns ride out as opaque JSON strings so the
// client can render the diff without the wire shape needing to grow
// every time schema.md gains a new payload variant. Empty / `{}`
// payloads round-trip as the literal `{}` string for symmetry with the
// migration default — clients should treat that as the empty case.
func auditLogToProto(r *repo.AuditLog) *pb.AuditLogEntry {
	out := &pb.AuditLogEntry{
		Id:         r.ID.String(),
		Namespace:  r.Namespace,
		Action:     r.Action,
		BeforeJson: rawJSONString(r.Before),
		AfterJson:  rawJSONString(r.After),
		CreatedAt:  timestamppb.New(r.CreatedAt),
	}
	if r.PlaytestID != nil {
		v := r.PlaytestID.String()
		out.PlaytestId = &v
	}
	if r.ActorUserID != nil {
		v := agsid.Format(*r.ActorUserID)
		out.ActorUserId = &v
	}
	return out
}

// rawJSONString stringifies a JSONB column for the wire. Empty bytes
// surface as `{}` so the field is always parseable JSON.
func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}
