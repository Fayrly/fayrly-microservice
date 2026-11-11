package event

import (
	"errors"
	"time"
)

var (
	ErrReviewNotFound    = errors.New("review not found")
	ErrReviewAlreadyMade = errors.New("you have already reviewed this event")
)

// Review is a public event review with the event summary needed by review
// cards outside the event detail page.
type Review struct {
	ID           string    `json:"id"`
	EventID      string    `json:"event_id"`
	EventSlug    string    `json:"event_slug"`
	EventTitle   string    `json:"event_title"`
	EventCover   string    `json:"event_cover_url"`
	EventStarts  time.Time `json:"event_starts_at"`
	UserID       string    `json:"user_id"`
	UserName     string    `json:"user_name"`
	Rating       int       `json:"rating"`
	Body         string    `json:"body"`
	PhotoURL     string    `json:"photo_url,omitempty"`
	HelpfulCount int       `json:"helpful_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type ReviewSummary struct {
	Count   int     `json:"count"`
	Average float64 `json:"average"`
}

type CreateReviewRequest struct {
	Rating   int    `json:"rating" validate:"required,gte=1,lte=5"`
	Body     string `json:"body" validate:"required,min=1,max=2000"`
	PhotoURL string `json:"photo_url" validate:"omitempty,url,max=500"`
}
