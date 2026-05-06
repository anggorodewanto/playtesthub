package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/anggorodewanto/playtesthub/pkg/dmqueue"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// DMEnqueuer is the slice of pkg/dmqueue.Queue the service depends on.
// Tests inject a fake; production wires *dmqueue.Queue (which already
// satisfies it).
type DMEnqueuer interface {
	Enqueue(ctx context.Context, j dmqueue.Job) error
}

// WithDMQueue attaches the DM queue used by ApproveApplicant + RetryDM
// (M2 phase 7). Calls fall back to silent skip when nil so unit tests
// that do not exercise DM behaviour can stay one-line constructions.
func (s *PlaytesthubServiceServer) WithDMQueue(q DMEnqueuer) *PlaytesthubServiceServer {
	s.dmQueue = q
	return s
}

// RetryDM re-attempts a Discord DM for an APPROVED applicant whose
// last DM is in the failed state (per dm-queue.md "Retry-DM gate").
// PRD §5.4: there is no cooldown — back-to-back clicks enqueue two
// jobs. The response carries the applicant row as-of the request; the
// worker updates last_dm_status asynchronously and the admin UI
// reflects the new state on the next list refresh.
func (s *PlaytesthubServiceServer) RetryDM(ctx context.Context, req *pb.RetryDMRequest) (*pb.RetryDMResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.dmQueue == nil {
		return nil, status.Error(codes.Internal, "dm queue not wired")
	}
	applicantID, err := uuid.Parse(req.GetApplicantId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "applicant_id is not a uuid: %v", err)
	}

	a, err := s.applicant.GetByID(ctx, applicantID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "applicant not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if a.Status != applicantStatusApproved {
		return nil, status.Error(codes.FailedPrecondition, "retry dm requires approved applicant")
	}
	if a.LastDMStatus == nil || *a.LastDMStatus != dmStatusFailed {
		return nil, status.Error(codes.FailedPrecondition, "retry dm requires last dm status=failed")
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, a.PlaytestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	enqueueErr := s.dmQueue.Enqueue(ctx, buildDMJob(a, pt, true))
	// The queue handles overflow internally (writes failed status +
	// audit and returns ErrQueueFull). We re-fetch so the response
	// shows the synchronous state change instead of the stale row.
	if errors.Is(enqueueErr, dmqueue.ErrQueueFull) {
		fresh, ferr := s.applicant.GetByID(ctx, applicantID)
		if ferr == nil {
			a = fresh
		}
	} else if enqueueErr != nil {
		return nil, status.Errorf(codes.Internal, "enqueuing dm: %v", enqueueErr)
	}
	return &pb.RetryDMResponse{Applicant: adminApplicantToProto(a)}, nil
}

// buildDMJob assembles the queue Job from an applicant + playtest.
// The recipient is applicant.discord_user_id (the Discord snowflake
// stamped at signup from the IAM `platform_user_id` claim per
// migration 0004). Rows persisted before that migration carry NULL —
// the queue surfaces those as `lastDmError='missing_recipient'`
// (docs/errors.md) without invoking the Discord client.
func buildDMJob(a *repo.Applicant, pt *repo.Playtest, manual bool) dmqueue.Job {
	var recipient string
	if a.DiscordUserID != nil {
		recipient = *a.DiscordUserID
	}
	return dmqueue.Job{
		ApplicantID:   a.ID,
		PlaytestID:    a.PlaytestID,
		UserID:        a.UserID,
		DiscordUserID: recipient,
		Message:       fmt.Sprintf("You're approved for %q. Open the playtest to view your code.", pt.Title),
		Manual:        manual,
	}
}
