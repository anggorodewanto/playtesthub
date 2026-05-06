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
)

// WithSurveyStore attaches the Survey repository required by the M3
// CreateSurvey / EditSurvey / GetSurvey handlers. Optional in M1/M2;
// the handlers below surface Internal when the store is unwired so
// pre-M3 unit tests keep their existing constructor calls.
func (s *PlaytesthubServiceServer) WithSurveyStore(ss repo.SurveyStore) *PlaytesthubServiceServer {
	s.survey = ss
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
		row.Type = "text"
		if len(q.GetOptions()) > 0 {
			return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options must be empty for text questions", idx)
		}
	case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING:
		row.Type = "rating"
		if len(q.GetOptions()) > 0 {
			return surveyQuestionRow{}, status.Errorf(codes.InvalidArgument, "questions[%d].options must be empty for rating questions", idx)
		}
	case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_MULTI_CHOICE:
		row.Type = "multi_choice"
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
	case "text":
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT
	case "rating":
		return pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING
	case "multi_choice":
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
