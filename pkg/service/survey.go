package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Survey CRUD bounds — schema.md §"Survey entity spec" + PRD §5.6.
const (
	maxSurveyQuestions     = 50
	maxSurveyPromptChars   = 1000
	multiChoiceMinOptions  = 2
	multiChoiceMaxOptions  = 20
	multiChoiceLabelMaxLen = 200
	surveyTextAnswerMax    = 4000 // PRD §5.6 — text answer max chars on submit.
	surveyRatingMin        = 1    // PRD §5.6 — rating fixed 1–5.
	surveyRatingMax        = 5
)

// Persisted question type tags. Match the SurveyQuestionType proto
// enum, lower-cased; pulled out so survey.go isn't peppered with the
// same string literal at every type switch site.
const (
	surveyTypeText        = "text"
	surveyTypeRating      = "rating"
	surveyTypeMultiChoice = "multi_choice"
)

// WithSurveyStore attaches the Survey repository required by the M3
// CreateSurvey / EditSurvey / GetSurvey handlers. Optional in M1/M2;
// the handlers below surface Internal when the store is unwired so
// pre-M3 unit tests keep their existing constructor calls.
func (s *PlaytesthubServiceServer) WithSurveyStore(ss repo.SurveyStore) *PlaytesthubServiceServer {
	s.survey = ss
	return s
}

// WithSurveyResponseStore attaches the SurveyResponse repository
// required by SubmitSurveyResponse (M3 phase 4). Optional in earlier
// milestones; SubmitSurveyResponse surfaces Internal when unwired.
func (s *PlaytesthubServiceServer) WithSurveyResponseStore(rs repo.SurveyResponseStore) *PlaytesthubServiceServer {
	s.surveyResponse = rs
	return s
}

// surveyQuestionRow is the on-disk shape of a single question. The
// repo layer treats Survey.Questions as opaque JSONB; this struct pins
// the field names + typed shape the service consumes. Marshalling here
// (rather than relying on protojson) keeps `id` and `options[].id`
// stable across edits and decouples the wire shape from the storage
// shape per schema.md.
type surveyQuestionRow struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Prompt        string                 `json:"prompt"`
	Required      bool                   `json:"required"`
	AllowMultiple bool                   `json:"allowMultiple,omitempty"`
	Options       []multiChoiceOptionRow `json:"options,omitempty"`
}

type multiChoiceOptionRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// CreateSurvey inserts the first-version survey for a playtest. Natural-
// key on playtest_id — a second call after a successful insert returns
// AlreadyExists per PRD §4.7. The server mints UUIDs for every question
// and multi-choice option (any client-supplied id is ignored on Create —
// EditSurvey is the preserve-on-edit path).
func (s *PlaytesthubServiceServer) CreateSurvey(ctx context.Context, req *pb.CreateSurveyRequest) (*pb.CreateSurveyResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.survey == nil {
		return nil, status.Error(codes.Internal, "survey store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
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

	rows, err := normaliseQuestionsForCreate(req.GetQuestions())
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshalling survey questions: %v", err)
	}

	created, err := s.survey.Create(ctx, playtestID, body)
	if errors.Is(err, repo.ErrUniqueViolation) {
		return nil, status.Errorf(codes.AlreadyExists, "survey already exists for playtest %s; use EditSurvey to update it", playtestID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating survey: %v", err)
	}

	if err := s.playtest.SetSurveyID(ctx, s.namespace, playtestID, created.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "pointing playtest survey_id: %v", err)
	}

	if s.audit != nil {
		if auditErr := repo.AppendSurveyCreate(ctx, s.audit, s.namespace, playtestID, created.ID, len(rows)); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending survey.create audit: %v", auditErr)
		}
	}

	resp, err := surveyToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering survey response: %v", err)
	}
	return &pb.CreateSurveyResponse{Survey: resp}, nil
}

// EditSurvey writes a new survey row with version = previous + 1.
// Question UUIDs are preserved for kept questions (client passes the
// existing id) and minted for new ones (id empty). Multi-choice option
// ids likewise — keeps histogram aggregation keys stable across edits
// per schema.md.
func (s *PlaytesthubServiceServer) EditSurvey(ctx context.Context, req *pb.EditSurveyRequest) (*pb.EditSurveyResponse, error) {
	actorID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.survey == nil {
		return nil, status.Error(codes.Internal, "survey store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
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

	previous, err := s.survey.GetCurrent(ctx, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Errorf(codes.FailedPrecondition, "no survey to edit on playtest %s; create one first", playtestID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching current survey: %v", err)
	}

	prevRows, err := decodeQuestionRows(previous.Questions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decoding stored survey questions: %v", err)
	}
	rows, err := normaliseQuestionsForEdit(req.GetQuestions(), prevRows)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(rows)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshalling survey questions: %v", err)
	}

	next, err := s.survey.EditAsNewVersion(ctx, playtestID, body)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Errorf(codes.FailedPrecondition, "no survey to edit on playtest %s; create one first", playtestID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "editing survey: %v", err)
	}

	if err := s.playtest.SetSurveyID(ctx, s.namespace, playtestID, next.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "pointing playtest survey_id: %v", err)
	}

	if s.audit != nil {
		if auditErr := repo.AppendSurveyEdit(ctx, s.audit, s.namespace, playtestID, actorID, previous.ID, next.ID, previous.Questions, next.Questions); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending survey.edit audit: %v", auditErr)
		}
	}

	resp, err := surveyToProto(next)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering survey response: %v", err)
	}
	return &pb.EditSurveyResponse{Survey: resp}, nil
}

// GetSurvey returns the current survey version pointed at by
// Playtest.surveyId. Authenticated player surface — DRAFT, soft-deleted,
// and CLOSED-without-approval playtests are NotFound (mirrors
// GetPlaytestForPlayer visibility), and a playtest without a configured
// survey is also NotFound.
func (s *PlaytesthubServiceServer) GetSurvey(ctx context.Context, req *pb.GetSurveyRequest) (*pb.GetSurveyResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if s.survey == nil {
		return nil, status.Error(codes.Internal, "survey store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil || pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if pt.Status == statusClosed {
		approved, err := s.isApprovedApplicant(ctx, pt.ID, userID)
		if err != nil {
			return nil, err
		}
		if !approved {
			return nil, status.Error(codes.NotFound, "playtest not found")
		}
	}
	if pt.SurveyID == nil {
		return nil, status.Error(codes.NotFound, "playtest has no survey configured")
	}
	got, err := s.survey.GetByID(ctx, *pt.SurveyID)
	if errors.Is(err, repo.ErrNotFound) {
		// playtest.survey_id points at a row that's gone — treat the
		// playtest as having no survey rather than leaking the dangling
		// pointer.
		return nil, status.Error(codes.NotFound, "playtest has no survey configured")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching survey: %v", err)
	}
	resp, err := surveyToProto(got)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering survey response: %v", err)
	}
	return &pb.GetSurveyResponse{Survey: resp}, nil
}

// normaliseQuestionsForCreate validates the incoming question array
// and assigns server-minted UUIDs to every question + multi-choice
// option. Any client-supplied id is dropped (the doc-of-truth says
// Create ignores client ids; preserve-on-edit is the EditSurvey path).
func normaliseQuestionsForCreate(in []*pb.SurveyQuestion) ([]surveyQuestionRow, error) {
	if err := checkQuestionCount(in); err != nil {
		return nil, err
	}
	out := make([]surveyQuestionRow, 0, len(in))
	for i, q := range in {
		row, err := validateAndShape(q, i)
		if err != nil {
			return nil, err
		}
		row.ID = uuid.NewString()
		for j := range row.Options {
			row.Options[j].ID = uuid.NewString()
		}
		out = append(out, row)
	}
	return out, nil
}

// normaliseQuestionsForEdit validates the incoming question array and
// preserves UUIDs for kept questions / options (client passes the
// existing id) while minting new ones for empty ids. Per schema.md,
// keeping ids stable across edits is what lets histogram aggregation
// keys remain stable across version bumps. Option-id preservation is
// independent of question-id preservation — moving an option from one
// question to another in an edit still aggregates the same response
// histogram bucket.
func normaliseQuestionsForEdit(in []*pb.SurveyQuestion, previous []surveyQuestionRow) ([]surveyQuestionRow, error) {
	if err := checkQuestionCount(in); err != nil {
		return nil, err
	}
	prevQ := make(map[string]struct{}, len(previous))
	prevOpt := make(map[string]struct{})
	for _, p := range previous {
		prevQ[p.ID] = struct{}{}
		for _, o := range p.Options {
			prevOpt[o.ID] = struct{}{}
		}
	}
	out := make([]surveyQuestionRow, 0, len(in))
	seenQ := make(map[string]struct{}, len(in))
	seenOpt := make(map[string]struct{})
	for i, q := range in {
		row, err := validateAndShape(q, i)
		if err != nil {
			return nil, err
		}
		if id := q.GetId(); id != "" {
			if _, dup := seenQ[id]; dup {
				return nil, status.Errorf(codes.InvalidArgument, "questions[%d].id %q duplicated in request", i, id)
			}
			if _, ok := prevQ[id]; !ok {
				return nil, status.Errorf(codes.InvalidArgument, "questions[%d].id %q does not match any question on the current survey version", i, id)
			}
			row.ID = id
			seenQ[id] = struct{}{}
		} else {
			row.ID = uuid.NewString()
		}
		for j, opt := range row.Options {
			if opt.ID != "" {
				if _, ok := prevOpt[opt.ID]; !ok {
					return nil, status.Errorf(codes.InvalidArgument, "questions[%d].options[%d].id %q does not match any option on the current survey version", i, j, opt.ID)
				}
				if _, dup := seenOpt[opt.ID]; dup {
					return nil, status.Errorf(codes.InvalidArgument, "questions[%d].options[%d].id %q duplicated in request", i, j, opt.ID)
				}
				seenOpt[opt.ID] = struct{}{}
				continue
			}
			row.Options[j].ID = uuid.NewString()
			seenOpt[row.Options[j].ID] = struct{}{}
		}
		out = append(out, row)
	}
	return out, nil
}

func checkQuestionCount(in []*pb.SurveyQuestion) error {
	if len(in) == 0 {
		return status.Error(codes.InvalidArgument, "questions must contain at least one entry")
	}
	if len(in) > maxSurveyQuestions {
		return status.Errorf(codes.InvalidArgument, "questions length %d exceeds the %d-question cap (schema.md §\"Survey entity spec\")", len(in), maxSurveyQuestions)
	}
	return nil
}

// validateAndShape projects a wire SurveyQuestion to the persisted
// shape and asserts the per-type bounds. The server is the only place
// these are enforced; clients pass through the wire types untouched.
func validateAndShape(q *pb.SurveyQuestion, idx int) (surveyQuestionRow, error) {
	prompt := q.GetPrompt()
	if prompt == "" {
		return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].prompt is required", idx)
	}
	if len([]rune(prompt)) > maxSurveyPromptChars {
		return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].prompt exceeds the %d-char cap", idx, maxSurveyPromptChars)
	}
	row := surveyQuestionRow{
		Prompt:        prompt,
		Required:      q.GetRequired(),
		AllowMultiple: q.GetAllowMultiple(),
	}
	switch q.GetType() {
	case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT:
		row.Type = surveyTypeText
		if len(q.GetOptions()) > 0 {
			return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options must be empty for text questions", idx)
		}
	case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING:
		row.Type = surveyTypeRating
		if len(q.GetOptions()) > 0 {
			return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options must be empty for rating questions", idx)
		}
	case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_MULTI_CHOICE:
		row.Type = surveyTypeMultiChoice
		if len(q.GetOptions()) < multiChoiceMinOptions || len(q.GetOptions()) > multiChoiceMaxOptions {
			return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options length %d outside the %d–%d bound (schema.md)", idx, len(q.GetOptions()), multiChoiceMinOptions, multiChoiceMaxOptions)
		}
		opts := make([]multiChoiceOptionRow, 0, len(q.GetOptions()))
		for j, opt := range q.GetOptions() {
			if opt.GetLabel() == "" {
				return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options[%d].label is required", idx, j)
			}
			if len([]rune(opt.GetLabel())) > multiChoiceLabelMaxLen {
				return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options[%d].label exceeds the %d-char cap", idx, j, multiChoiceLabelMaxLen)
			}
			opts = append(opts, multiChoiceOptionRow{ID: opt.GetId(), Label: opt.GetLabel()})
		}
		row.Options = opts
	default:
		return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].type is required (text | rating | multi_choice)", idx)
	}
	return row, nil
}

func decodeQuestionRows(raw json.RawMessage) ([]surveyQuestionRow, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var rows []surveyQuestionRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decoding question rows: %w", err)
	}
	return rows, nil
}

// surveyToProto renders a stored Survey as the wire message. Question
// rows are decoded back into the wire shape — the JSONB on disk is the
// internal shape (lower-case json tags); the wire shape is the proto
// SurveyQuestionType enum + repeated MultiChoiceOption.
func surveyToProto(s *repo.Survey) (*pb.Survey, error) {
	rows, err := decodeQuestionRows(s.Questions)
	if err != nil {
		return nil, fmt.Errorf("decoding survey questions: %w", err)
	}
	out := &pb.Survey{
		Id:         s.ID.String(),
		PlaytestId: s.PlaytestID.String(),
		Version:    int32(s.Version),
		CreatedAt:  timestamppb.New(s.CreatedAt),
	}
	for _, r := range rows {
		out.Questions = append(out.Questions, &pb.SurveyQuestion{
			Id:            r.ID,
			Type:          questionTypeStringToEnum(r.Type),
			Prompt:        r.Prompt,
			Required:      r.Required,
			AllowMultiple: r.AllowMultiple,
			Options:       optionsToProto(r.Options),
		})
	}
	return out, nil
}

func questionTypeStringToEnum(t string) pb.SurveyQuestionType {
	switch t {
	case surveyTypeText:
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT
	case surveyTypeRating:
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING
	case surveyTypeMultiChoice:
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_MULTI_CHOICE
	default:
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_UNSPECIFIED
	}
}

func optionsToProto(in []multiChoiceOptionRow) []*pb.MultiChoiceOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]*pb.MultiChoiceOption, 0, len(in))
	for _, o := range in {
		out = append(out, &pb.MultiChoiceOption{Id: o.ID, Label: o.Label})
	}
	return out
}

// surveyAnswerRow is the on-disk shape of a single answer. Matches the
// proto SurveyAnswer oneof (text / rating / multi_choice) but stored
// as a flat JSONB document keyed on questionId. Field names mirror the
// proto wire shape so the admin responses viewer can decode without an
// extra translation layer.
type surveyAnswerRow struct {
	QuestionID  string   `json:"questionId"`
	Text        string   `json:"text,omitempty"`
	Rating      int32    `json:"rating,omitempty"`
	MultiChoice []string `json:"multiChoice,omitempty"`
}

// SubmitSurveyResponse records a single one-shot answer set for the
// calling player. Idempotent on `(playtestId, userId)`: a second call
// returns gRPC `AlreadyExists` with an empty body per [`errors.md`]
// row 31 — the original submission's answers are never overwritten or
// echoed (PRD §5.6 / §6 redaction).
//
// Gating mirrors [`AcceptNDA`] / [`GetSurvey`]: the caller must hold an
// applicant row, must be APPROVED, and the applicant's stamped NDA
// hash must equal the playtest's current hash when NDA is required
// (server-checked, never client-asserted). The submitted `surveyId`
// must belong to the playtest but is not required to match the
// current version — concurrent edits do not invalidate an in-flight
// submit (PRD §5.6 mid-fill version race).
func (s *PlaytesthubServiceServer) SubmitSurveyResponse(ctx context.Context, req *pb.SubmitSurveyResponseRequest) (*pb.SubmitSurveyResponseResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if s.survey == nil || s.surveyResponse == nil {
		return nil, status.Error(codes.Internal, "survey response store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	surveyID, err := parseReqUUID("survey_id", req.GetSurveyId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil || pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	app, err := s.applicant.GetByPlaytestUser(ctx, playtestID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.FailedPrecondition, "must signup before submitting a survey response")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if app.Status != applicantStatusApproved {
		return nil, status.Error(codes.FailedPrecondition, "only APPROVED applicants may submit a survey response")
	}
	if pt.NDARequired {
		if app.NDAVersionHash == nil || *app.NDAVersionHash != pt.CurrentNDAVersionHash {
			return nil, status.Error(codes.FailedPrecondition, "NDA re-acceptance required before submitting a survey response")
		}
	}

	if pt.SurveyID == nil {
		return nil, status.Error(codes.FailedPrecondition, "playtest has no survey configured")
	}

	survey, err := s.survey.GetByID(ctx, surveyID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Errorf(codes.InvalidArgument, "survey_id %s does not exist", surveyID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching survey: %v", err)
	}
	if survey.PlaytestID != playtestID {
		return nil, status.Errorf(codes.InvalidArgument, "survey_id %s does not belong to playtest %s", surveyID, playtestID)
	}

	rows, err := decodeQuestionRows(survey.Questions)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decoding stored survey questions: %v", err)
	}
	answerRows, err := validateAndShapeAnswers(req.GetAnswers(), rows)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"answers": answerRows})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshalling survey answers: %v", err)
	}

	resp, replay, err := s.surveyResponse.SubmitOnce(ctx, &repo.SurveyResponse{
		PlaytestID: playtestID,
		UserID:     userID,
		SurveyID:   surveyID,
		Answers:    body,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "submitting survey response: %v", err)
	}
	if replay {
		// Per errors.md row 31: the second submit returns AlreadyExists
		// with an empty body. The server intentionally does NOT echo
		// the original answers (PRD §6 redaction — survey free-text
		// answers must not appear on the wire after submit).
		return nil, status.Error(codes.AlreadyExists, "survey already submitted for this playtest")
	}

	out, err := surveyResponseToProto(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering survey response: %v", err)
	}
	return &pb.SubmitSurveyResponseResponse{Response: out}, nil
}

// validateAndShapeAnswers asserts that every required question is
// answered, that each answer matches its question's type + bounds
// (text ≤ 4,000 chars, rating in [1,5], multi-choice option ids are
// known to the question, allow_multiple respected), and rejects
// answers that target a question outside the survey or repeat a
// question id. Returns the persisted rows in question order so the
// stored shape mirrors the survey for trivial admin reads.
func validateAndShapeAnswers(in []*pb.SurveyAnswer, questions []surveyQuestionRow) ([]surveyAnswerRow, error) {
	byID := make(map[string]surveyQuestionRow, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}
	answered := make(map[string]*pb.SurveyAnswer, len(in))
	for i, a := range in {
		if a.GetQuestionId() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "answers[%d].question_id is required", i)
		}
		if _, dup := answered[a.GetQuestionId()]; dup {
			return nil, status.Errorf(codes.InvalidArgument, "answers[%d].question_id %q is repeated", i, a.GetQuestionId())
		}
		if _, ok := byID[a.GetQuestionId()]; !ok {
			return nil, status.Errorf(codes.InvalidArgument, "answers[%d].question_id %q does not match any question on the survey", i, a.GetQuestionId())
		}
		answered[a.GetQuestionId()] = a
	}

	out := make([]surveyAnswerRow, 0, len(questions))
	for qIdx, q := range questions {
		ans, present := answered[q.ID]
		if !present {
			if q.Required {
				return nil, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) is required and has no answer", qIdx, q.ID)
			}
			continue
		}
		row, err := shapeAnswer(qIdx, q, ans)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func shapeAnswer(qIdx int, q surveyQuestionRow, a *pb.SurveyAnswer) (surveyAnswerRow, error) {
	switch q.Type {
	case surveyTypeText:
		return shapeTextAnswer(qIdx, q, a)
	case surveyTypeRating:
		return shapeRatingAnswer(qIdx, q, a)
	case surveyTypeMultiChoice:
		return shapeMultiChoiceAnswer(qIdx, q, a)
	}
	return surveyAnswerRow{}, status.Errorf(codes.Internal, "questions[%d] (%q) has unknown type %q", qIdx, q.ID, q.Type)
}

func shapeTextAnswer(qIdx int, q surveyQuestionRow, a *pb.SurveyAnswer) (surveyAnswerRow, error) {
	text := a.GetText()
	if q.Required && text == "" {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) is required; text answer is empty", qIdx, q.ID)
	}
	if len([]rune(text)) > surveyTextAnswerMax {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) text answer exceeds the %d-char cap", qIdx, q.ID, surveyTextAnswerMax)
	}
	if _, ok := a.GetValue().(*pb.SurveyAnswer_Text); !ok && (q.Required || text != "") {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) expects a text answer", qIdx, q.ID)
	}
	return surveyAnswerRow{QuestionID: q.ID, Text: text}, nil
}

func shapeRatingAnswer(qIdx int, q surveyQuestionRow, a *pb.SurveyAnswer) (surveyAnswerRow, error) {
	if _, ok := a.GetValue().(*pb.SurveyAnswer_Rating); !ok {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) expects a rating answer", qIdx, q.ID)
	}
	r := a.GetRating()
	if r < surveyRatingMin || r > surveyRatingMax {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) rating %d outside the %d–%d range", qIdx, q.ID, r, surveyRatingMin, surveyRatingMax)
	}
	return surveyAnswerRow{QuestionID: q.ID, Rating: r}, nil
}

func shapeMultiChoiceAnswer(qIdx int, q surveyQuestionRow, a *pb.SurveyAnswer) (surveyAnswerRow, error) {
	mc, ok := a.GetValue().(*pb.SurveyAnswer_MultiChoice)
	if !ok || mc.MultiChoice == nil {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) expects a multi_choice answer", qIdx, q.ID)
	}
	picked := mc.MultiChoice.GetOptionIds()
	if q.Required && len(picked) == 0 {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) is required; multi_choice answer is empty", qIdx, q.ID)
	}
	if !q.AllowMultiple && len(picked) > 1 {
		return surveyAnswerRow{}, status.Errorf(codes.InvalidArgument, "questions[%d] (%q) is single-select; multi_choice answer has %d entries", qIdx, q.ID, len(picked))
	}
	if err := validateMultiChoicePicks(qIdx, q, picked); err != nil {
		return surveyAnswerRow{}, err
	}
	return surveyAnswerRow{QuestionID: q.ID, MultiChoice: picked}, nil
}

func validateMultiChoicePicks(qIdx int, q surveyQuestionRow, picked []string) error {
	validIDs := make(map[string]struct{}, len(q.Options))
	for _, opt := range q.Options {
		validIDs[opt.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(picked))
	for k, id := range picked {
		if _, ok := validIDs[id]; !ok {
			return status.Errorf(codes.InvalidArgument, "questions[%d] (%q).option_ids[%d] %q does not match any option on the question", qIdx, q.ID, k, id)
		}
		if _, dup := seen[id]; dup {
			return status.Errorf(codes.InvalidArgument, "questions[%d] (%q).option_ids[%d] %q is repeated", qIdx, q.ID, k, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

// ListSurveyResponses returns one page of SurveyResponse rows for the
// playtest, ordered (submittedAt, id) DESC, DESC per repo.SurveyResponseStore.
// Optional `survey_id_filter` narrows to a single Survey version
// (per-version aggregate split per PRD §5.6). Soft-deleted playtests
// are NotFound; admin endpoint, requires actor + namespace match.
//
// Pagination: page_size 0 → server default (50), capped at 200 by the
// repo layer. Malformed page_token → InvalidArgument (mirrors
// ListApplicants).
func (s *PlaytesthubServiceServer) ListSurveyResponses(ctx context.Context, req *pb.ListSurveyResponsesRequest) (*pb.ListSurveyResponsesResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.surveyResponse == nil {
		return nil, status.Error(codes.Internal, "survey response store not configured")
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
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

	var surveyFilter uuid.UUID
	if raw := req.GetSurveyIdFilter(); raw != "" {
		surveyFilter, err = uuid.Parse(raw)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "survey_id_filter is not a uuid: %v", err)
		}
	}

	page, err := s.surveyResponse.ListResponses(ctx, repo.SurveyResponsePageQuery{
		PlaytestID: playtestID,
		SurveyID:   surveyFilter,
		PageToken:  req.GetPageToken(),
		Limit:      int(req.GetPageSize()),
	})
	if errors.Is(err, repo.ErrInvalidSurveyResponseToken) {
		return nil, status.Error(codes.InvalidArgument, "page_token is malformed")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing survey responses: %v", err)
	}

	out := make([]*pb.SurveyResponse, 0, len(page.Rows))
	for _, r := range page.Rows {
		rendered, renderErr := surveyResponseToProto(r)
		if renderErr != nil {
			return nil, status.Errorf(codes.Internal, "rendering survey response: %v", renderErr)
		}
		out = append(out, rendered)
	}
	return &pb.ListSurveyResponsesResponse{Responses: out, NextPageToken: page.NextPageToken}, nil
}

// surveyResponseToProto renders a stored SurveyResponse as the wire
// message. Re-decodes the JSONB document into the proto oneof shape so
// the consumer receives the typed answers, not the on-disk JSON.
func surveyResponseToProto(r *repo.SurveyResponse) (*pb.SurveyResponse, error) {
	out := &pb.SurveyResponse{
		Id:          r.ID.String(),
		PlaytestId:  r.PlaytestID.String(),
		UserId:      r.UserID.String(),
		SurveyId:    r.SurveyID.String(),
		SubmittedAt: timestamppb.New(r.SubmittedAt),
	}
	if len(r.Answers) > 0 {
		var doc struct {
			Answers []surveyAnswerRow `json:"answers"`
		}
		if err := json.Unmarshal(r.Answers, &doc); err != nil {
			return nil, fmt.Errorf("decoding survey response answers: %w", err)
		}
		for _, a := range doc.Answers {
			out.Answers = append(out.Answers, answerRowToProto(a))
		}
	}
	return out, nil
}

func answerRowToProto(a surveyAnswerRow) *pb.SurveyAnswer {
	out := &pb.SurveyAnswer{QuestionId: a.QuestionID}
	switch {
	case len(a.MultiChoice) > 0:
		out.Value = &pb.SurveyAnswer_MultiChoice{
			MultiChoice: &pb.SurveyMultiChoiceAnswer{OptionIds: a.MultiChoice},
		}
	case a.Rating != 0:
		out.Value = &pb.SurveyAnswer_Rating{Rating: a.Rating}
	default:
		out.Value = &pb.SurveyAnswer_Text{Text: a.Text}
	}
	return out
}
