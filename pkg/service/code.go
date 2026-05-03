package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// PRD §4.3 — STEAM_KEYS upload bounds.
const (
	uploadCSVMaxBytes  = 10 * 1024 * 1024
	uploadCSVMaxRows   = 50_000
	uploadCodeMinChars = 1
	uploadCodeMaxChars = 128

	// Audit row reasons (schema.md L52). One reason per code.upload_rejected
	// row — the response body lists per-line reasons separately.
	uploadReasonNonUTF8       = "non_utf8"
	uploadReasonSizeExceeded  = "size_exceeded"
	uploadReasonCountExceeded = "count_exceeded"
	uploadReasonCharset       = "charset_violation"
	uploadReasonLength        = "length_violation"
	uploadReasonEmpty         = "empty_line"
	uploadReasonDupInFile     = "duplicate_in_file"
	uploadReasonDupInDB       = "duplicate_in_db"
)

// codeCharset matches PRD §4.3: [A-Za-z0-9._-].
var codeCharset = regexp.MustCompile(`^[A-Za-z0-9._\-]+$`)

// utf8BOM is stripped from the head of the CSV before line-splitting
// per PRD §4.3 ("UTF-8 only; leading BOM stripped").
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// WithCodeStore attaches the Code repository required by UploadCodes
// + GetCodePool (M2 phase 5). Calls fall back to Internal when nil so
// pre-phase-5 unit tests can still construct the server.
func (s *PlaytesthubServiceServer) WithCodeStore(c repo.CodeStore) *PlaytesthubServiceServer {
	s.code = c
	return s
}

// UploadCodes ingests a CSV of STEAM_KEYS code values for an admin-
// owned playtest. PRD §4.3:
//   - UTF-8 only (BOM stripped); ≤10 MB; ≤50,000 codes; per-code length
//     1–128 within charset [A-Za-z0-9._-]; whitespace trimmed.
//   - File-level + cross-row dedup (duplicate within the file *and*
//     against existing Code rows).
//   - Whole-file reject on any violation; the response carries the
//     offending line numbers / values; zero rows committed.
//   - Concurrency: dedup + insert run in one tx holding the per-
//     playtest pg_advisory_xact_lock (handled by repo.UploadAtomic).
//
// The audit row is `code.upload` on success, `code.upload_rejected`
// on a whole-file reject (system-emitted; no actor — schema.md L51).
func (s *PlaytesthubServiceServer) UploadCodes(ctx context.Context, req *pb.UploadCodesRequest) (*pb.UploadCodesResponse, error) {
	actorID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.code == nil {
		return nil, status.Error(codes.Internal, "code store not wired")
	}
	playtestID, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if pt.DistributionModel != distModelSteamKeys {
		return nil, status.Error(codes.FailedPrecondition, "UploadCodes requires distribution_model=STEAM_KEYS (this playtest is AGS_CAMPAIGN; use TopUpCodes/SyncFromAGS)")
	}

	csv := req.GetCsvContent()
	filename := req.GetFilename()

	// File-size cap is the very first check — even a 1GB junk payload
	// bottoms out here without burning memory parsing it.
	if len(csv) > uploadCSVMaxBytes {
		s.recordUploadRejected(ctx, pt.ID, filename, uploadReasonSizeExceeded, 0)
		return nil, status.Errorf(codes.InvalidArgument,
			"csv_content exceeds %d bytes (got %d)", uploadCSVMaxBytes, len(csv))
	}
	if !utf8.Valid(csv) {
		s.recordUploadRejected(ctx, pt.ID, filename, uploadReasonNonUTF8, 0)
		return nil, status.Error(codes.InvalidArgument,
			"csv_content is not valid UTF-8")
	}

	parsed, parseRejections, parseRowCount := parseUploadCSV(csv)

	if parseRowCount > uploadCSVMaxRows {
		s.recordUploadRejected(ctx, pt.ID, filename, uploadReasonCountExceeded, parseRowCount)
		return nil, status.Errorf(codes.InvalidArgument,
			"csv_content has %d codes, exceeds limit of %d", parseRowCount, uploadCSVMaxRows)
	}
	if len(parseRejections) > 0 {
		// PRD §4.3: whole-file reject — return the per-line list,
		// don't surface the gRPC error code (the proto carries the
		// rejections in-band so the admin UI can render them).
		topReason := dominantReason(parseRejections)
		s.recordUploadRejected(ctx, pt.ID, filename, topReason, parseRowCount)
		return &pb.UploadCodesResponse{
			Inserted:   0,
			Rejections: rejectionsToProto(parseRejections),
		}, nil
	}
	if len(parsed) == 0 {
		// Empty file or all-whitespace — nothing to insert. Treat as a
		// no-op success rather than a reject; admin can re-upload.
		return &pb.UploadCodesResponse{Inserted: 0}, nil
	}

	values := make([]string, len(parsed))
	for i, p := range parsed {
		values[i] = p.value
	}

	inserted, dups, err := s.code.UploadAtomic(ctx, pt.ID, values)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "uploading codes: %v", err)
	}
	if len(dups) > 0 {
		dupRejections := mapDupsToLines(parsed, dups)
		s.recordUploadRejected(ctx, pt.ID, filename, uploadReasonDupInDB, parseRowCount)
		return &pb.UploadCodesResponse{
			Inserted:   0,
			Rejections: rejectionsToProto(dupRejections),
		}, nil
	}

	if s.audit != nil {
		sum := sha256.Sum256(csv)
		if auditErr := repo.AppendCodeUpload(ctx, s.audit, s.namespace, pt.ID, actorID, inserted, hex.EncodeToString(sum[:]), filename); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending code.upload audit: %v", auditErr)
		}
	}

	return &pb.UploadCodesResponse{Inserted: int32(inserted)}, nil
}

// GetCodePool returns aggregate counts plus the full list of code
// rows including raw values (PRD §5.7 page 4 — admin surfaces are
// exempt from the §6 log-redaction rule). Soft-deleted playtests are
// hidden.
func (s *PlaytesthubServiceServer) GetCodePool(ctx context.Context, req *pb.GetCodePoolRequest) (*pb.GetCodePoolResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.code == nil {
		return nil, status.Error(codes.Internal, "code store not wired")
	}
	playtestID, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	rows, err := s.code.ListByPlaytest(ctx, pt.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing codes: %v", err)
	}
	stats := codePoolStats(rows)
	out := make([]*pb.Code, 0, len(rows))
	for _, r := range rows {
		out = append(out, codeToProto(r))
	}
	return &pb.GetCodePoolResponse{Stats: stats, Codes: out}, nil
}

// recordUploadRejected logs a code.upload_rejected audit row.
// Best-effort: an audit failure does not roll back the user-facing
// rejection — the upload was rejected anyway. Logged silently when
// audit store is not wired (M1-era unit tests).
func (s *PlaytesthubServiceServer) recordUploadRejected(ctx context.Context, playtestID uuid.UUID, filename, reason string, rowCount int) {
	if s.audit == nil {
		return
	}
	_ = repo.AppendCodeUploadRejected(ctx, s.audit, s.namespace, playtestID, filename, reason, rowCount)
}

// parsedLine carries a 1-based CSV line number alongside its value so
// the service can map dedup-against-DB collisions back to the source
// line for the admin response.
type parsedLine struct {
	line  int
	value string
}

// uploadRejection mirrors pb.UploadCodesRejection so the service can
// build it without crossing the proto/repo boundary.
type uploadRejection struct {
	line   int
	reason string
	value  string
}

// parseUploadCSV applies the PRD §4.3 per-line rules:
//   - strip leading UTF-8 BOM;
//   - split on \n (CRLF tolerated — \r trimmed in the trim step);
//   - skip empty trailing newline;
//   - trim whitespace; reject empty after trim;
//   - validate length 1–128;
//   - validate charset [A-Za-z0-9._\-];
//   - flag duplicates within the file;
//
// Returns the surviving (parsedLine, []uploadRejection, totalRowCount)
// triple. parseRowCount is the count of *content* rows considered (not
// including the trailing empty line that comes from a final \n).
func parseUploadCSV(b []byte) ([]parsedLine, []uploadRejection, int) {
	if bytesHasBOMPrefix(b) {
		b = b[len(utf8BOM):]
	}
	text := string(b)
	// Split keeps empty trailing element when text ends in \n; we drop
	// it so a normal "line\nline\n" file isn't penalised for the final
	// newline.
	rawLines := strings.Split(text, "\n")
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	parsed := make([]parsedLine, 0, len(rawLines))
	rejections := make([]uploadRejection, 0)
	seen := make(map[string]int, len(rawLines))
	for i, raw := range rawLines {
		line := i + 1
		v := strings.TrimSpace(raw)
		if v == "" {
			rejections = append(rejections, uploadRejection{line: line, reason: uploadReasonEmpty})
			continue
		}
		if len(v) < uploadCodeMinChars || len(v) > uploadCodeMaxChars {
			rejections = append(rejections, uploadRejection{line: line, reason: uploadReasonLength, value: v})
			continue
		}
		if !codeCharset.MatchString(v) {
			rejections = append(rejections, uploadRejection{line: line, reason: uploadReasonCharset, value: v})
			continue
		}
		if firstLine, dup := seen[v]; dup {
			rejections = append(rejections, uploadRejection{line: line, reason: uploadReasonDupInFile, value: v})
			// Note: the first occurrence is kept in `parsed`; subsequent
			// dups are rejected. The whole-file-reject rule means we
			// won't insert anyway when len(rejections) > 0.
			_ = firstLine
			continue
		}
		seen[v] = line
		parsed = append(parsed, parsedLine{line: line, value: v})
	}
	return parsed, rejections, len(rawLines)
}

func bytesHasBOMPrefix(b []byte) bool {
	if len(b) < len(utf8BOM) {
		return false
	}
	for i, v := range utf8BOM {
		if b[i] != v {
			return false
		}
	}
	return true
}

// dominantReason picks the audit-row reason from a list of per-line
// rejections. Priority order matches admin debugging value: charset >
// length > duplicate-in-file > empty.
func dominantReason(rejections []uploadRejection) string {
	priority := map[string]int{
		uploadReasonCharset:   4,
		uploadReasonLength:    3,
		uploadReasonDupInFile: 2,
		uploadReasonEmpty:     1,
	}
	best := ""
	bestScore := -1
	for _, r := range rejections {
		if score := priority[r.reason]; score > bestScore {
			best = r.reason
			bestScore = score
		}
	}
	if best == "" {
		return uploadReasonCharset
	}
	return best
}

// mapDupsToLines turns the dedup-against-DB string list back into
// per-line rejections so the admin response carries the offending
// CSV line numbers. The same value appearing on multiple input lines
// (intra-file dup) would have been caught at parse time, so we expect
// each value to map to exactly one parsed line.
func mapDupsToLines(parsed []parsedLine, dups []string) []uploadRejection {
	dupSet := make(map[string]bool, len(dups))
	for _, d := range dups {
		dupSet[d] = true
	}
	out := make([]uploadRejection, 0, len(dups))
	for _, p := range parsed {
		if dupSet[p.value] {
			out = append(out, uploadRejection{line: p.line, reason: uploadReasonDupInDB, value: p.value})
		}
	}
	return out
}

func rejectionsToProto(rs []uploadRejection) []*pb.UploadCodesRejection {
	out := make([]*pb.UploadCodesRejection, 0, len(rs))
	for _, r := range rs {
		out = append(out, &pb.UploadCodesRejection{
			LineNumber: int32(r.line),
			Reason:     r.reason,
			Value:      r.value,
		})
	}
	return out
}

func codePoolStats(rows []*repo.Code) *pb.CodePoolStats {
	var unused, reserved, granted int32
	for _, r := range rows {
		switch r.State {
		case repo.CodeStateUnused:
			unused++
		case repo.CodeStateReserved:
			reserved++
		case repo.CodeStateGranted:
			granted++
		}
	}
	return &pb.CodePoolStats{
		Total:    int32(len(rows)),
		Unused:   unused,
		Reserved: reserved,
		Granted:  granted,
	}
}

func codeToProto(c *repo.Code) *pb.Code {
	out := &pb.Code{
		Id:         c.ID.String(),
		PlaytestId: c.PlaytestID.String(),
		Value:      c.Value,
		State:      codeStateStringToEnum(c.State),
		CreatedAt:  timeToTimestamp(&c.CreatedAt),
	}
	if c.ReservedBy != nil {
		v := c.ReservedBy.String()
		out.ReservedBy = &v
	}
	if c.ReservedAt != nil {
		out.ReservedAt = timeToTimestamp(c.ReservedAt)
	}
	if c.GrantedAt != nil {
		out.GrantedAt = timeToTimestamp(c.GrantedAt)
	}
	return out
}

func codeStateStringToEnum(s string) pb.CodeState {
	switch s {
	case repo.CodeStateUnused:
		return pb.CodeState_CODE_STATE_UNUSED
	case repo.CodeStateReserved:
		return pb.CodeState_CODE_STATE_RESERVED
	case repo.CodeStateGranted:
		return pb.CodeState_CODE_STATE_GRANTED
	}
	return pb.CodeState_CODE_STATE_UNSPECIFIED
}
