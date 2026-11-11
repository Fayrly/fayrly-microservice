// Package event implements concert, charity and community event management.
package event

import (
	"errors"
	"time"
)

var (
	ErrEventNotFound           = errors.New("event not found")
	ErrSlugTaken               = errors.New("slug is already in use")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
	ErrEventLocked             = errors.New("completed or cancelled events can no longer be edited")
)

const (
	TypeConcert   = "concert"
	TypeCharity   = "charity"
	TypeCommunity = "community"

	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusOngoing   = "ongoing"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

type Event struct {
	ID             string     `json:"id"`
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	VenueName      string     `json:"venue_name"`
	VenueAddress   string     `json:"venue_address"`
	City           string     `json:"city"`
	Country        string     `json:"country"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at,omitempty"`
	Capacity       int        `json:"capacity"`
	PriceMin       int64      `json:"price_min"`
	PriceMax       int64      `json:"price_max"`
	Currency       string     `json:"currency"`
	CoverImageURL  string     `json:"cover_image_url"`
	OrganizerName  string     `json:"organizer_name"`
	ContactEmail   string     `json:"contact_email"`
	ContactPhone   string     `json:"contact_phone"`
	Tags           []string   `json:"tags"`
	Beneficiary    string     `json:"beneficiary,omitempty"`
	DonationTarget int64      `json:"donation_target,omitempty"`
	DonationRaised int64      `json:"donation_raised,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedByName  string     `json:"created_by_name"`
	UpdatedBy      *string    `json:"updated_by,omitempty"`
	PublishedAt    *time.Time `json:"published_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	GoingCount  int      `json:"going_count"`
	GoingNames  []string `json:"going_names"`
	ReviewCount int      `json:"review_count"`
	RatingAvg   float64  `json:"rating_avg"`
}

type Filter struct {
	Query      string
	Type       string
	City       string
	Statuses   []string
	CreatedBy  string
	StartsFrom *time.Time
	StartsTo   *time.Time
}

type CreateEventRequest struct {
	Title          string     `json:"title" validate:"required,min=3,max=200"`
	Slug           string     `json:"slug" validate:"omitempty,max=120"`
	Description    string     `json:"description" validate:"omitempty,max=20000"`
	Type           string     `json:"type" validate:"required,oneof=concert charity community"`
	VenueName      string     `json:"venue_name" validate:"omitempty,max=200"`
	VenueAddress   string     `json:"venue_address" validate:"omitempty,max=500"`
	City           string     `json:"city" validate:"omitempty,max=120"`
	Country        string     `json:"country" validate:"omitempty,len=2"`
	StartsAt       *time.Time `json:"starts_at" validate:"required"`
	EndsAt         *time.Time `json:"ends_at"`
	Capacity       int        `json:"capacity" validate:"gte=0,lte=1000000"`
	PriceMin       int64      `json:"price_min" validate:"gte=0"`
	PriceMax       int64      `json:"price_max" validate:"gte=0"`
	Currency       string     `json:"currency" validate:"omitempty,len=3"`
	CoverImageURL  string     `json:"cover_image_url" validate:"omitempty,url,max=500"`
	OrganizerName  string     `json:"organizer_name" validate:"omitempty,max=200"`
	ContactEmail   string     `json:"contact_email" validate:"omitempty,email,max=254"`
	ContactPhone   string     `json:"contact_phone" validate:"omitempty,max=32"`
	Tags           []string   `json:"tags" validate:"omitempty,max=10,dive,min=1,max=40"`
	Beneficiary    string     `json:"beneficiary" validate:"omitempty,max=200"`
	DonationTarget int64      `json:"donation_target" validate:"gte=0"`
	DonationRaised int64      `json:"donation_raised" validate:"gte=0"`
}

type UpdateEventRequest struct {
	Title          *string    `json:"title" validate:"omitempty,min=3,max=200"`
	Slug           *string    `json:"slug" validate:"omitempty,max=120"`
	Description    *string    `json:"description" validate:"omitempty,max=20000"`
	Type           *string    `json:"type" validate:"omitempty,oneof=concert charity community"`
	VenueName      *string    `json:"venue_name" validate:"omitempty,max=200"`
	VenueAddress   *string    `json:"venue_address" validate:"omitempty,max=500"`
	City           *string    `json:"city" validate:"omitempty,max=120"`
	Country        *string    `json:"country" validate:"omitempty,len=2"`
	StartsAt       *time.Time `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	Capacity       *int       `json:"capacity" validate:"omitempty,gte=0,lte=1000000"`
	PriceMin       *int64     `json:"price_min" validate:"omitempty,gte=0"`
	PriceMax       *int64     `json:"price_max" validate:"omitempty,gte=0"`
	Currency       *string    `json:"currency" validate:"omitempty,len=3"`
	CoverImageURL  *string    `json:"cover_image_url" validate:"omitempty,url,max=500"`
	OrganizerName  *string    `json:"organizer_name" validate:"omitempty,max=200"`
	ContactEmail   *string    `json:"contact_email" validate:"omitempty,email,max=254"`
	ContactPhone   *string    `json:"contact_phone" validate:"omitempty,max=32"`
	Tags           *[]string  `json:"tags" validate:"omitempty,max=10,dive,min=1,max=40"`
	Beneficiary    *string    `json:"beneficiary" validate:"omitempty,max=200"`
	DonationTarget *int64     `json:"donation_target" validate:"omitempty,gte=0"`
	DonationRaised *int64     `json:"donation_raised" validate:"omitempty,gte=0"`
}

type StatusRequest struct {
	Status string `json:"status" validate:"required,oneof=published ongoing completed cancelled"`
}
