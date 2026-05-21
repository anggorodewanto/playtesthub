package service

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func (s *PlaytesthubServiceServer) GetPlaytestParticipants(ctx context.Context, req *pb.GetPlaytestParticipantsRequest) (*pb.GetPlaytestParticipantsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	rows, err := s.applicant.ListByPlaytest(ctx, playtestID, applicantStatusFilterFromPb(req.GetStatusFilter()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing participants: %v", err)
	}

	out := make([]*pb.ParticipantRow, 0, len(rows))
	for _, a := range rows {
		out = append(out, applicantToParticipantRow(a))
	}
	return &pb.GetPlaytestParticipantsResponse{Participants: out}, nil
}

// applicantStatusFilterFromPb maps the proto status enum to the DB
// status string used by applicant queries. UNSPECIFIED and unknown
// values map to "" (no filter) — callers that need strict rejection
// validate separately.
func applicantStatusFilterFromPb(v pb.ApplicantStatus) string {
	switch v {
	case pb.ApplicantStatus_APPLICANT_STATUS_PENDING:
		return applicantStatusPending
	case pb.ApplicantStatus_APPLICANT_STATUS_APPROVED:
		return applicantStatusApproved
	case pb.ApplicantStatus_APPLICANT_STATUS_REJECTED:
		return applicantStatusRejected
	}
	return ""
}

func applicantToParticipantRow(a *repo.Applicant) *pb.ParticipantRow {
	row := &pb.ParticipantRow{
		ApplicantId:   a.ID.String(),
		UserId:        agsid.Format(a.UserID),
		DiscordHandle: a.DiscordHandle,
		SignupAt:      timestamppb.New(a.CreatedAt),
		Status:        applicantStatusStringToEnum(a.Status),
		AutoApproved:  a.AutoApproved,
	}
	// nda_accepted_at is a proxy until the per-applicant modal needs the
	// real NDAAcceptance.accepted_at — the table only renders a checkmark
	// (PRD §5.4 / M5.C scope).
	if a.NDAVersionHash != nil {
		row.NdaAcceptedAt = timestamppb.New(a.CreatedAt)
	}
	if a.LastDMStatus != nil && *a.LastDMStatus == dmStatusSent && a.LastDMAttemptAt != nil {
		row.CodeSentAt = timestamppb.New(*a.LastDMAttemptAt)
	}
	return row
}
