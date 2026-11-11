package report

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/fayrly/backend/internal/platform/database"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

const reportSelect = `SELECT id::text AS id, title, type, event_id::text AS event_id, event_title,
	to_char(period_start, 'YYYY-MM-DD') AS period_start,
	to_char(period_end, 'YYYY-MM-DD') AS period_end,
	summary, metrics, status, created_by::text AS created_by, created_by_name, submitted_at,
	reviewed_by::text AS reviewed_by, reviewed_by_name, reviewed_at, review_notes,
	created_at, updated_at
	FROM reports`

type reportRow struct {
	ID             string
	Title          string
	Type           string
	EventID        *string
	EventTitle     string
	PeriodStart    *string
	PeriodEnd      *string
	Summary        string
	Metrics        database.JSON
	Status         string
	CreatedBy      string
	CreatedByName  string
	SubmittedAt    *time.Time
	ReviewedBy     *string
	ReviewedByName string
	ReviewedAt     *time.Time
	ReviewNotes    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (m reportRow) toDomain() *Report {
	metrics := json.RawMessage(m.Metrics)
	if len(metrics) == 0 {
		metrics = json.RawMessage(`{}`)
	}
	return &Report{
		ID:             m.ID,
		Title:          m.Title,
		Type:           m.Type,
		EventID:        m.EventID,
		EventTitle:     m.EventTitle,
		PeriodStart:    m.PeriodStart,
		PeriodEnd:      m.PeriodEnd,
		Summary:        m.Summary,
		Metrics:        metrics,
		Status:         m.Status,
		CreatedBy:      m.CreatedBy,
		CreatedByName:  m.CreatedByName,
		SubmittedAt:    m.SubmittedAt,
		ReviewedBy:     m.ReviewedBy,
		ReviewedByName: m.ReviewedByName,
		ReviewedAt:     m.ReviewedAt,
		ReviewNotes:    m.ReviewNotes,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Report, error) {
	var row reportRow
	err := r.db.WithContext(ctx).Raw(
		reportSelect+" WHERE id = ? AND deleted_at IS NULL", id).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, ErrReportNotFound
	}
	return row.toDomain(), nil
}

func (r *Repository) Create(ctx context.Context, report *Report) error {
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO reports (
			id, title, type, event_id, event_title, period_start, period_end, summary,
			metrics, status, created_by, created_by_name
		) VALUES (
			?::uuid, ?, ?, ?::uuid, ?, ?::date, ?::date, ?,
			?::jsonb, ?, ?::uuid, ?
		)`,
		report.ID, report.Title, report.Type, report.EventID, report.EventTitle,
		report.PeriodStart, report.PeriodEnd, report.Summary, string(metricsOrDefault(report.Metrics)),
		report.Status, report.CreatedBy, report.CreatedByName).Error
}

type updateFields struct {
	Title       *string
	Type        *string
	EventID     *string
	EventTitle  *string
	PeriodStart *string
	PeriodEnd   *string
	Summary     *string
	Metrics     *string
}

func (r *Repository) Update(ctx context.Context, id string, f updateFields) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE reports SET
			title        = COALESCE(?::text, title),
			type         = COALESCE(?::text, type),
			event_id     = COALESCE(?::uuid, event_id),
			event_title  = COALESCE(?::text, event_title),
			period_start = COALESCE(?::date, period_start),
			period_end   = COALESCE(?::date, period_end),
			summary      = COALESCE(?::text, summary),
			metrics      = COALESCE(?::jsonb, metrics),
			updated_at   = now()
		 WHERE id = ? AND deleted_at IS NULL`,
		f.Title, f.Type, f.EventID, f.EventTitle, f.PeriodStart, f.PeriodEnd,
		f.Summary, f.Metrics, id).Error
}

func (r *Repository) Submit(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(
		`UPDATE reports SET status = 'submitted', submitted_at = now(), updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL AND status IN ('draft', 'rejected')`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInvalidReportStatus
	}
	return nil
}

func (r *Repository) Review(ctx context.Context, id, decision, reviewerID, reviewerName, notes string, at time.Time) error {
	result := r.db.WithContext(ctx).Exec(
		`UPDATE reports SET
			status = ?,
			reviewed_by = ?::uuid,
			reviewed_by_name = ?,
			reviewed_at = ?,
			review_notes = ?,
			updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL AND status = 'submitted'`,
		decision, reviewerID, reviewerName, at, notes, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInvalidReportStatus
	}
	return nil
}

func (r *Repository) SoftDelete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(
		`UPDATE reports SET deleted_at = now(), updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL AND status IN ('draft', 'rejected')`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrDeleteNotAllowed
	}
	return nil
}

func (r *Repository) List(ctx context.Context, filter Filter, limit, offset int) ([]*Report, int, error) {
	var args []any
	where := []string{"deleted_at IS NULL"}

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		where = append(where, "(title ILIKE ? OR summary ILIKE ? OR event_title ILIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}
	if filter.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.EventID != "" {
		where = append(where, "event_id = ?")
		args = append(args, filter.EventID)
	}
	if filter.CreatedBy != "" {
		where = append(where, "created_by = ?")
		args = append(args, filter.CreatedBy)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Table("reports").Where(whereSQL, args...).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []reportRow
	err := r.db.WithContext(ctx).Raw(
		reportSelect+" WHERE "+whereSQL+" ORDER BY created_at DESC, id LIMIT ? OFFSET ?",
		append(args, limit, offset)...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	reports := make([]*Report, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, row.toDomain())
	}
	return reports, int(total), nil
}

func metricsOrDefault(metrics json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(metrics))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(trimmed)
}
