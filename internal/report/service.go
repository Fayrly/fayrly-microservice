package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

const maxMetricsBytes = 64 * 1024

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, filter Filter, page httpx.Page) ([]*Report, int, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Type = strings.ToLower(strings.TrimSpace(filter.Type))
	filter.Status = strings.ToLower(strings.TrimSpace(filter.Status))
	filter.EventID = strings.TrimSpace(filter.EventID)

	if filter.Type != "" && !validType(filter.Type) {
		return nil, 0, apierr.Validation(map[string]string{"type": "must be one of: financial, attendance, incident, impact, other"})
	}
	if filter.Status != "" && !validStatus(filter.Status) {
		return nil, 0, apierr.Validation(map[string]string{"status": "must be one of: draft, submitted, approved, rejected"})
	}
	if filter.EventID != "" {
		if _, err := uuid.Parse(filter.EventID); err != nil {
			return nil, 0, apierr.Validation(map[string]string{"event_id": "must be a valid UUID"})
		}
	}

	reports, total, err := s.repo.List(ctx, filter, page.Size, page.Offset())
	if err != nil {
		return nil, 0, mapReportError(err)
	}
	return reports, total, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Report, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, apierr.NotFound("report")
	}
	report, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapReportError(err)
	}
	return report, nil
}

func (s *Service) Create(ctx context.Context, principal *authn.Principal, req CreateReportRequest) (*Report, error) {
	metrics, err := normalizeMetrics(req.Metrics)
	if err != nil {
		return nil, err
	}
	periodStart, periodEnd, err := normalizePeriod(req.PeriodStart, req.PeriodEnd)
	if err != nil {
		return nil, err
	}

	report := &Report{
		ID:            uuid.NewString(),
		Title:         strings.TrimSpace(req.Title),
		Type:          req.Type,
		EventID:       normalizeEventID(req.EventID),
		EventTitle:    strings.TrimSpace(req.EventTitle),
		PeriodStart:   periodStart,
		PeriodEnd:     periodEnd,
		Summary:       strings.TrimSpace(req.Summary),
		Metrics:       metrics,
		Status:        StatusDraft,
		CreatedBy:     principal.UserID,
		CreatedByName: principal.Name,
	}
	if err := s.repo.Create(ctx, report); err != nil {
		return nil, mapReportError(err)
	}
	return s.repo.GetByID(ctx, report.ID)
}

func (s *Service) Update(ctx context.Context, principal *authn.Principal, id string, req UpdateReportRequest) (*Report, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != StatusDraft && existing.Status != StatusRejected {
		return nil, apierr.Conflict(ErrReportLocked.Error())
	}
	if existing.CreatedBy != principal.UserID {
		return nil, apierr.Forbidden(ErrNotReportAuthor.Error())
	}

	fields := updateFields{}
	start := existing.PeriodStart
	end := existing.PeriodEnd

	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		fields.Title = &value
	}
	if req.Type != nil {
		value := strings.ToLower(strings.TrimSpace(*req.Type))
		fields.Type = &value
	}
	if req.EventID != nil {
		fields.EventID = normalizeEventID(req.EventID)
	}
	if req.EventTitle != nil {
		value := strings.TrimSpace(*req.EventTitle)
		fields.EventTitle = &value
	}
	if req.PeriodStart != nil {
		value := strings.TrimSpace(*req.PeriodStart)
		if value != "" {
			fields.PeriodStart = &value
			start = &value
		}
	}
	if req.PeriodEnd != nil {
		value := strings.TrimSpace(*req.PeriodEnd)
		if value != "" {
			fields.PeriodEnd = &value
			end = &value
		}
	}
	if req.Summary != nil {
		value := strings.TrimSpace(*req.Summary)
		fields.Summary = &value
	}
	if req.Metrics != nil {
		metrics, err := normalizeMetrics(req.Metrics)
		if err != nil {
			return nil, err
		}
		encoded := string(metrics)
		fields.Metrics = &encoded
	}

	if err := validatePeriod(start, end); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, id, fields); err != nil {
		return nil, mapReportError(err)
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Delete(ctx context.Context, principal *authn.Principal, id string) error {
	report, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if report.Status != StatusDraft && report.Status != StatusRejected {
		return apierr.Conflict(ErrDeleteNotAllowed.Error())
	}
	if report.CreatedBy != principal.UserID && !principal.Has("report:delete") {
		return apierr.Forbidden("you can only delete your own draft or rejected reports")
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return mapReportError(err)
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, principal *authn.Principal, id string) (*Report, error) {
	report, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if report.CreatedBy != principal.UserID {
		return nil, apierr.Forbidden(ErrNotReportAuthor.Error())
	}
	if report.Status != StatusDraft && report.Status != StatusRejected {
		return nil, apierr.Conflict(ErrInvalidReportStatus.Error())
	}
	if err := s.repo.Submit(ctx, id); err != nil {
		return nil, mapReportError(err)
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Review(ctx context.Context, principal *authn.Principal, id string, req ReviewRequest) (*Report, error) {
	report, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if report.Status != StatusSubmitted {
		return nil, apierr.Conflict("only submitted reports can be reviewed")
	}
	if report.CreatedBy == principal.UserID {
		return nil, apierr.Forbidden(ErrSelfReview.Error())
	}
	notes := strings.TrimSpace(req.Notes)
	if req.Decision == StatusRejected && notes == "" {
		return nil, apierr.Validation(map[string]string{"notes": ErrReviewNotesRequired.Error()})
	}

	if err := s.repo.Review(ctx, id, req.Decision, principal.UserID, principal.Name, notes, time.Now().UTC()); err != nil {
		return nil, mapReportError(err)
	}
	return s.repo.GetByID(ctx, id)
}

func normalizeMetrics(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(trimmed) > maxMetricsBytes {
		return nil, apierr.Validation(map[string]string{"metrics": "must not exceed 64 KiB"})
	}
	if trimmed[0] != '{' {
		return nil, apierr.Validation(map[string]string{"metrics": "must be a JSON object"})
	}
	var object map[string]any
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return nil, apierr.Validation(map[string]string{"metrics": "must be a valid JSON object"})
	}
	return json.RawMessage(trimmed), nil
}

func normalizePeriod(start, end *string) (*string, *string, error) {
	normalizedStart := trimOptional(start)
	normalizedEnd := trimOptional(end)
	if err := validatePeriod(normalizedStart, normalizedEnd); err != nil {
		return nil, nil, err
	}
	return normalizedStart, normalizedEnd, nil
}

func validatePeriod(start, end *string) error {
	startDate, err := parsePeriodValue(start, "period_start")
	if err != nil {
		return err
	}
	endDate, err := parsePeriodValue(end, "period_end")
	if err != nil {
		return err
	}
	if startDate != nil && endDate != nil && endDate.Before(*startDate) {
		return apierr.Validation(map[string]string{"period_end": "must be on or after period_start"})
	}
	return nil
}

func parsePeriodValue(value *string, field string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*value))
	if err != nil {
		return nil, apierr.Validation(map[string]string{field: "must use format YYYY-MM-DD"})
	}
	return &parsed, nil
}

func trimOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeEventID(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validType(value string) bool {
	switch value {
	case TypeFinancial, TypeAttendance, TypeIncident, TypeImpact, TypeOther:
		return true
	}
	return false
}

func validStatus(value string) bool {
	switch value {
	case StatusDraft, StatusSubmitted, StatusApproved, StatusRejected:
		return true
	}
	return false
}

func mapReportError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	switch {
	case errors.Is(err, ErrReportNotFound):
		return apierr.NotFound("report")
	case errors.Is(err, ErrReportLocked):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrInvalidReportStatus):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrDeleteNotAllowed):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrSelfReview):
		return apierr.Forbidden(err.Error())
	case errors.Is(err, ErrNotReportAuthor):
		return apierr.Forbidden(err.Error())
	default:
		return err
	}
}
