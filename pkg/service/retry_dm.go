package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

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

// WithPlayerBaseURL sets the public origin of the player Svelte bundle.
// When non-empty, the approval DM includes a deep link to the pending
// page so recipients jump straight to the granted-code view. Empty
// preserves the legacy non-clickable DM body.
func (s *PlaytesthubServiceServer) WithPlayerBaseURL(u string) *PlaytesthubServiceServer {
	s.playerBaseURL = u
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
	applicantID, err := parseReqUUID("applicant_id", req.GetApplicantId())
	if err != nil {
		return nil, err
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
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	adtURLs, err := s.maybeResolveADTURL(ctx, pt, a)
	if err != nil {
		return nil, err
	}
	enqueueErr := s.dmQueue.Enqueue(ctx, buildDMJob(a, pt, true, s.playerBaseURL, adtURLs))
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

// RetryFailedDms walks every APPROVED applicant whose last_dm_status
// is 'failed' for the playtest and enqueues each through the same
// DM-queue path as approve. Bulk variant of RetryDM (PRD §5.5 /
// dm-queue.md "Bulk retry RPC"). Not idempotent: calling twice
// enqueues two jobs per still-failed applicant — operators are
// expected to invoke this exactly once after a Discord outage.
//
// Overflow semantics mirror approve-time: the queue marks
// `lastDmError='dm_queue_overflow'` synchronously inside Enqueue and
// returns ErrQueueFull, which surfaces in the response as the
// `overflow` count. Once overflow starts, every subsequent enqueue in
// the same call is also rejected — Enqueue is non-blocking.
func (s *PlaytesthubServiceServer) RetryFailedDms(ctx context.Context, req *pb.RetryFailedDmsRequest) (*pb.RetryFailedDmsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.dmQueue == nil {
		return nil, status.Error(codes.Internal, "dm queue not wired")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	rows, err := s.applicant.ListDMFailedByPlaytest(ctx, pt.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing dm-failed applicants: %v", err)
	}

	var enqueued, overflow int32
	for _, a := range rows {
		adtURLs, urlErr := s.maybeResolveADTURL(ctx, pt, a)
		if urlErr != nil {
			return nil, urlErr
		}
		enqErr := s.dmQueue.Enqueue(ctx, buildDMJob(a, pt, true, s.playerBaseURL, adtURLs))
		if errors.Is(enqErr, dmqueue.ErrQueueFull) {
			overflow++
			continue
		}
		if enqErr != nil {
			return nil, status.Errorf(codes.Internal, "enqueuing dm: %v", enqErr)
		}
		enqueued++
	}
	return &pb.RetryFailedDmsResponse{Enqueued: enqueued, Overflow: overflow}, nil
}

// maybeResolveADTURL re-mints a fresh ADT download URL list when the
// playtest is ADT-distribution; returns nil for the other models.
// RetryDM + RetryFailedDms call it so the queued DM carries non-stale
// URLs — the previous ones may have expired.
func (s *PlaytesthubServiceServer) maybeResolveADTURL(ctx context.Context, pt *repo.Playtest, a *repo.Applicant) ([]string, error) {
	if pt.DistributionModel != distModelADT {
		return nil, nil
	}
	urls, _, err := s.resolveADTDownloadURL(ctx, pt, a)
	return urls, err
}

// buildDMJob assembles the queue Job from an applicant + playtest.
// The recipient is applicant.discord_user_id (the Discord snowflake
// stamped at signup from the IAM `platform_user_id` claim per
// migration 0004). Rows persisted before that migration carry NULL —
// the queue surfaces those as `lastDmError='missing_recipient'`
// (docs/errors.md) without invoking the Discord client.
//
// playerBaseURL is the public origin of the player Svelte bundle (e.g.
// "https://anggorodewanto.github.io/playtesthub"). When non-empty, the
// DM body includes a deep link to the pending page so the recipient
// taps once and lands on the granted-code view (one further Discord
// re-auth on the player domain may be required, but no manual
// navigation). When empty the message falls back to non-clickable copy.
func buildDMJob(a *repo.Applicant, pt *repo.Playtest, manual bool, playerBaseURL string, adtDownloadURLs []string) dmqueue.Job {
	var recipient string
	if a.DiscordUserID != nil {
		recipient = *a.DiscordUserID
	}
	return dmqueue.Job{
		ApplicantID:   a.ID,
		PlaytestID:    a.PlaytestID,
		UserID:        a.UserID,
		DiscordUserID: recipient,
		Message:       buildApprovalDMBody(pt, playerBaseURL, adtDownloadURLs),
		Manual:        manual,
	}
}

// buildApprovalDMBody renders the welcome DM text. With a configured
// playerBaseURL the link points at the hash-router pending route so a
// Discord client renders it as a tappable URL. The slug is URL-escaped
// to keep the output well-formed even if a future PRD revision relaxes
// slug validation; current PRD §5.1 only allows characters that survive
// PathEscape unchanged.
//
// ADT distribution (M5.B / dm-queue.md "DM body shape — ADT"): when
// pt.DistributionModel == "ADT" the body lists every resolved download
// URL. Single-file builds → "Download your playtest build for %q: %s".
// Multi-asset builds → "Download your playtest build for %q:\n1) %s\n
// 2) %s" so the recipient sees one tappable link per asset. RetryDM
// re-mints fresh URLs because the previous ones may have expired.
func buildApprovalDMBody(pt *repo.Playtest, playerBaseURL string, adtDownloadURLs []string) string {
	if pt.DistributionModel == distModelADT {
		return buildADTApprovalDMBody(pt.Title, adtDownloadURLs)
	}
	if playerBaseURL == "" {
		return fmt.Sprintf("You're approved for %q. Open the playtest to view your code.", pt.Title)
	}
	link := fmt.Sprintf("%s/#/playtest/%s/pending", playerBaseURL, url.PathEscape(pt.Slug))
	return fmt.Sprintf("You're approved for %q. View your code: %s", pt.Title, link)
}

// buildADTApprovalDMBody renders the ADT-flavoured DM body. Single-URL
// builds keep the historical one-line copy; multi-URL builds render a
// numbered list (`1) <url>` per line) so each asset shows as a
// separate tappable link on Discord.
func buildADTApprovalDMBody(title string, urls []string) string {
	if len(urls) <= 1 {
		var u string
		if len(urls) == 1 {
			u = urls[0]
		}
		return fmt.Sprintf("Download your playtest build for %q: %s", title, u)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Download your playtest build for %q:", title)
	for i, u := range urls {
		fmt.Fprintf(&b, "\n%d) %s", i+1, u)
	}
	return b.String()
}
