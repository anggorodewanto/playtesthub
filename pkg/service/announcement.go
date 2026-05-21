package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Subject/message bounds exposed so tests can pin the byte-exact
// errors.md strings. PRD §5.9 lists the defaults; env vars override
// via WithAnnouncementBounds.
const (
	defaultAnnouncementSubjectMaxLen = 200
	defaultAnnouncementMessageMaxLen = 4000
)

// AnnouncementSender posts a single direct-message body to a recipient.
// Production wiring is `pkg/discord.BotClient`. The inline fan-out path
// bypasses the M2 DM queue on purpose — the queue's circuit breaker
// protects the approve DM channel (per-applicant attribution on
// `applicant.last_dm_*`), which announcements must not touch.
type AnnouncementSender interface {
	SendDM(ctx context.Context, discordUserID, message string) error
}

func (s *PlaytesthubServiceServer) WithAnnouncementStore(store repo.AnnouncementStore, sender AnnouncementSender) *PlaytesthubServiceServer {
	s.announcement = store
	s.announcementSender = sender
	return s
}

func (s *PlaytesthubServiceServer) WithAnnouncementBounds(subjectMax, messageMax int) *PlaytesthubServiceServer {
	if subjectMax > 0 {
		s.announcementSubjectMaxLen = subjectMax
	}
	if messageMax > 0 {
		s.announcementMessageMaxLen = messageMax
	}
	return s
}

// CreateAnnouncement materialises an admin-authored bulk DM broadcast.
// Recipients resolve AT CALL TIME — applicants added after this call
// are NOT auto-included (PRD §5.4).
func (s *PlaytesthubServiceServer) CreateAnnouncement(ctx context.Context, req *pb.CreateAnnouncementRequest) (*pb.CreateAnnouncementResponse, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.announcement == nil {
		return nil, status.Error(codes.Internal, "announcement store not wired")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	filter, err := announcementSendToFilterFromPb(req.GetSendToFilter())
	if err != nil {
		return nil, err
	}
	if err := s.validateAnnouncementBody(req.GetSubject(), req.GetMessage()); err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusClosed {
		return nil, status.Error(codes.FailedPrecondition, "playtest is closed; announcements can no longer be sent")
	}

	recipients, err := s.resolveAnnouncementRecipients(ctx, playtestID, filter)
	if err != nil {
		return nil, err
	}

	saved, err := s.announcement.Insert(ctx, &repo.Announcement{
		PlaytestID:      playtestID,
		SendToFilter:    filter,
		Subject:         req.GetSubject(),
		Message:         req.GetMessage(),
		Status:          repo.AnnouncementStatusSending,
		RecipientsTotal: int32(len(recipients)),
		CreatedByUserID: actor,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "saving announcement: %v", err)
	}

	applicantIDs := make([]uuid.UUID, 0, len(recipients))
	for _, a := range recipients {
		applicantIDs = append(applicantIDs, a.ID)
	}
	if err := s.announcement.InsertRecipients(ctx, saved.ID, applicantIDs); err != nil {
		return nil, status.Errorf(codes.Internal, "saving recipients: %v", err)
	}
	if s.audit != nil {
		if err := repo.AppendAnnouncementCreate(ctx, s.audit, s.namespace, actor, saved.ID, playtestID, filter, int32(len(recipients))); err != nil {
			s.loggerOrDefault().Warn("appending announcement.create audit failed", "error", err.Error())
		}
	}

	s.fanOutAnnouncement(ctx, saved, recipients)

	if err := s.announcement.FinaliseStatus(ctx, saved.ID); err != nil {
		s.loggerOrDefault().Warn("finalising announcement status failed", "error", err.Error())
	}

	final, err := s.announcement.GetByID(ctx, saved.ID)
	if err != nil {
		final = saved
	}

	return &pb.CreateAnnouncementResponse{Announcement: announcementToProto(final)}, nil
}

func (s *PlaytesthubServiceServer) ListAnnouncements(ctx context.Context, req *pb.ListAnnouncementsRequest) (*pb.ListAnnouncementsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.announcement == nil {
		return nil, status.Error(codes.Internal, "announcement store not wired")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	rows, err := s.announcement.ListByPlaytest(ctx, playtestID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing announcements: %v", err)
	}
	out := make([]*pb.Announcement, 0, len(rows))
	for _, r := range rows {
		out = append(out, announcementToProto(r))
	}
	return &pb.ListAnnouncementsResponse{Announcements: out}, nil
}

func (s *PlaytesthubServiceServer) validateAnnouncementBody(subject, message string) error {
	subjectMax := s.announcementSubjectMaxLen
	if subjectMax <= 0 {
		subjectMax = defaultAnnouncementSubjectMaxLen
	}
	messageMax := s.announcementMessageMaxLen
	if messageMax <= 0 {
		messageMax = defaultAnnouncementMessageMaxLen
	}
	if len(subject) == 0 {
		return status.Error(codes.InvalidArgument, "announcement subject must not be empty")
	}
	if len(message) == 0 {
		return status.Error(codes.InvalidArgument, "announcement message must not be empty")
	}
	if len(subject) > subjectMax {
		return status.Errorf(codes.InvalidArgument, "announcement subject must be at most %d characters", subjectMax)
	}
	if len(message) > messageMax {
		return status.Errorf(codes.InvalidArgument, "announcement message must be at most %d characters", messageMax)
	}
	return nil
}

// resolveAnnouncementRecipients reads applicants matching the filter at
// call time. PRD §5.4 "Bulk announcements" — resolution is intentionally
// not a stored snapshot.
func (s *PlaytesthubServiceServer) resolveAnnouncementRecipients(ctx context.Context, playtestID uuid.UUID, filter string) ([]*repo.Applicant, error) {
	statusFilter := ""
	switch filter {
	case repo.AnnouncementSendToApprovedOnly:
		statusFilter = applicantStatusApproved
	case repo.AnnouncementSendToPendingOnly:
		statusFilter = applicantStatusPending
	}
	rows, err := s.applicant.ListByPlaytest(ctx, playtestID, statusFilter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing applicants: %v", err)
	}
	return rows, nil
}

// fanOutAnnouncement sends every recipient inline. Sender errors are
// captured on the per-recipient row. Missing recipient ids leave the
// row marked FAILED with a short tag (no Discord call attempted).
func (s *PlaytesthubServiceServer) fanOutAnnouncement(ctx context.Context, ann *repo.Announcement, recipients []*repo.Applicant) {
	now := time.Now().UTC()
	body := formatAnnouncementBody(ann.Subject, ann.Message)
	for _, a := range recipients {
		if a.DiscordUserID == nil || *a.DiscordUserID == "" {
			_ = s.announcement.MarkRecipientFailed(ctx, ann.ID, a.ID, now, "missing_recipient")
			continue
		}
		if s.announcementSender == nil {
			// No wiring → treat as ready-to-retry. Keeps the recipient
			// row at QUEUED so a future retry path (M6) can pick it up.
			continue
		}
		err := s.announcementSender.SendDM(ctx, *a.DiscordUserID, body)
		if err != nil {
			_ = s.announcement.MarkRecipientFailed(ctx, ann.ID, a.ID, now, classifyAnnouncementSendError(err))
			continue
		}
		_ = s.announcement.MarkRecipientSent(ctx, ann.ID, a.ID, now)
	}
}

func classifyAnnouncementSendError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "send_error"
}

func formatAnnouncementBody(subject, message string) string {
	return fmt.Sprintf("%s\n\n%s", subject, message)
}

func announcementSendToFilterFromPb(v pb.AnnouncementSendToFilter) (string, error) {
	switch v {
	case pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_ALL:
		return repo.AnnouncementSendToAll, nil
	case pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_APPROVED_ONLY:
		return repo.AnnouncementSendToApprovedOnly, nil
	case pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_PENDING_ONLY:
		return repo.AnnouncementSendToPendingOnly, nil
	}
	return "", status.Error(codes.InvalidArgument, "send_to_filter must be ALL, APPROVED_ONLY, or PENDING_ONLY")
}

func announcementSendToFilterToPb(v string) pb.AnnouncementSendToFilter {
	switch v {
	case repo.AnnouncementSendToAll:
		return pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_ALL
	case repo.AnnouncementSendToApprovedOnly:
		return pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_APPROVED_ONLY
	case repo.AnnouncementSendToPendingOnly:
		return pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_PENDING_ONLY
	}
	return pb.AnnouncementSendToFilter_ANNOUNCEMENT_SEND_TO_FILTER_UNSPECIFIED
}

func announcementStatusToPb(v string) pb.AnnouncementStatus {
	switch v {
	case repo.AnnouncementStatusSending:
		return pb.AnnouncementStatus_ANNOUNCEMENT_STATUS_SENDING
	case repo.AnnouncementStatusSent:
		return pb.AnnouncementStatus_ANNOUNCEMENT_STATUS_SENT
	case repo.AnnouncementStatusPartial:
		return pb.AnnouncementStatus_ANNOUNCEMENT_STATUS_PARTIAL
	case repo.AnnouncementStatusFailed:
		return pb.AnnouncementStatus_ANNOUNCEMENT_STATUS_FAILED
	}
	return pb.AnnouncementStatus_ANNOUNCEMENT_STATUS_UNSPECIFIED
}

func announcementToProto(a *repo.Announcement) *pb.Announcement {
	return &pb.Announcement{
		Id:              a.ID.String(),
		PlaytestId:      a.PlaytestID.String(),
		SendToFilter:    announcementSendToFilterToPb(a.SendToFilter),
		Subject:         a.Subject,
		Message:         a.Message,
		Status:          announcementStatusToPb(a.Status),
		RecipientsTotal: a.RecipientsTotal,
		RecipientsSent:  a.RecipientsSent,
		CreatedByUserId: agsid.Format(a.CreatedByUserID),
		CreatedAt:       timestamppb.New(a.CreatedAt),
	}
}
