// Package report implements event and programme reports with a submit/review
// approval workflow.
package report

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrReportNotFound      = errors.New("report not found")
	ErrReportLocked        = errors.New("only draft or rejected reports can be edited")
	ErrInvalidReportStatus = errors.New("the report is not in a state that allows this action")
	ErrSelfReview          = errors.New("you cannot review your own report")
	ErrReviewNotesRequired = errors.New("review notes are required when rejecting a report")
	ErrNotReportAuthor     = errors.New("only the report author can perform this action")
	ErrDeleteNotAllowed    = errors.New("only draft or rejected reports can be deleted")
)

const (
	TypeFinancial  = "financial"
	TypeAttendance = "attendance"
	TypeIncident   = "incident"
	TypeImpact     = "impact"
	TypeOther      = "other"

	StatusDraft     = "draft"
	StatusSubmitted = "submitted"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
)

type Report struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Type           string          `json:"type"`
	EventID        *string         `json:"event_id,omitempty"`
	EventTitle     string          `json:"event_title"`
	PeriodStart    *string         `json:"period_start,omitempty"`
	PeriodEnd      *string         `json:"period_end,omitempty"`
	Summary        string          `json:"summary"`
	Metrics        json.RawMessage `json:"metrics"`
	Status         string          `json:"status"`
	CreatedBy      string          `json:"created_by"`
	CreatedByName  string          `json:"created_by_name"`
	SubmittedAt    *time.Time      `json:"submitted_at,omitempty"`
	ReviewedBy     *string         `json:"reviewed_by,omitempty"`
	ReviewedByName string          `json:"reviewed_by_name,omitempty"`
	ReviewedAt     *time.Time      `json:"reviewed_at,omitempty"`
	ReviewNotes    string          `json:"review_notes,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type Filter struct {
	Query     string
	Type      string
	Status    string
	EventID   string
	CreatedBy string
}

type CreateReportRequest struct {
	Title       string          `json:"title" validate:"required,min=3,max=200"`
	Type        string          `json:"type" validate:"required,oneof=financial attendance incident impact other"`
	EventID     *string         `json:"event_id" validate:"omitempty,uuid"`
	EventTitle  string          `json:"event_title" validate:"omitempty,max=200"`
	PeriodStart *string         `json:"period_start"`
	PeriodEnd   *string         `json:"period_end"`
	Summary     string          `json:"summary" validate:"omitempty,max=10000"`
	Metrics     json.RawMessage `json:"metrics"`
}

type UpdateReportRequest struct {
	Title       *string         `json:"title" validate:"omitempty,min=3,max=200"`
	Type        *string         `json:"type" validate:"omitempty,oneof=financial attendance incident impact other"`
	EventID     *string         `json:"event_id" validate:"omitempty,uuid"`
	EventTitle  *string         `json:"event_title" validate:"omitempty,max=200"`
	PeriodStart *string         `json:"period_start"`
	PeriodEnd   *string         `json:"period_end"`
	Summary     *string         `json:"summary" validate:"omitempty,max=10000"`
	Metrics     json.RawMessage `json:"metrics"`
}

type ReviewRequest struct {
	Decision string `json:"decision" validate:"required,oneof=approved rejected"`
	Notes    string `json:"notes" validate:"max=2000"`
}
