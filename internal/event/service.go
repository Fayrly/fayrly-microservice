package event

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
	slugify "github.com/fayrly/backend/internal/platform/slug"
)

var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	countryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
)

var publicStatuses = []string{StatusPublished, StatusOngoing, StatusCompleted}

var allowedTransitions = map[string]map[string]bool{
	StatusDraft:     {StatusPublished: true, StatusCancelled: true},
	StatusPublished: {StatusOngoing: true, StatusCancelled: true},
	StatusOngoing:   {StatusCompleted: true, StatusCancelled: true},
	StatusCompleted: {},
	StatusCancelled: {},
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) List(ctx context.Context, principal *authn.Principal, filter Filter, page httpx.Page) ([]*Event, int, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Type = strings.ToLower(strings.TrimSpace(filter.Type))
	filter.City = strings.TrimSpace(filter.City)
	if filter.Type != "" && !validType(filter.Type) {
		return nil, 0, apierr.Validation(map[string]string{"type": "must be one of: concert, charity, community"})
	}

	statuses := make([]string, 0, len(filter.Statuses))
	for _, status := range filter.Statuses {
		status = strings.ToLower(strings.TrimSpace(status))
		if !validStatus(status) {
			return nil, 0, apierr.Validation(map[string]string{"status": "must be one of: draft, published, ongoing, completed, cancelled"})
		}
		statuses = append(statuses, status)
	}

	if !canReadAll(principal) {
		if len(statuses) == 0 {
			statuses = append([]string{}, publicStatuses...)
		} else {
			for _, status := range statuses {
				if !isPublicStatus(status) {
					return nil, 0, apierr.Forbidden("you do not have permission to list events with status " + status)
				}
			}
		}
	}
	filter.Statuses = statuses

	events, total, err := s.repo.List(ctx, filter, page.Size, page.Offset())
	if err != nil {
		return nil, 0, mapEventError(err)
	}
	return events, total, nil
}

func (s *Service) ListMine(ctx context.Context, principal *authn.Principal, filter Filter, page httpx.Page) ([]*Event, int, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Type = strings.ToLower(strings.TrimSpace(filter.Type))
	if filter.Type != "" && !validType(filter.Type) {
		return nil, 0, apierr.Validation(map[string]string{"type": "must be one of: concert, charity, community"})
	}
	statuses := make([]string, 0, len(filter.Statuses))
	for _, status := range filter.Statuses {
		status = strings.ToLower(strings.TrimSpace(status))
		if !validStatus(status) {
			return nil, 0, apierr.Validation(map[string]string{"status": "must be one of: draft, published, ongoing, completed, cancelled"})
		}
		statuses = append(statuses, status)
	}
	filter.Statuses = statuses
	filter.CreatedBy = principal.UserID

	events, total, err := s.repo.List(ctx, filter, page.Size, page.Offset())
	if err != nil {
		return nil, 0, mapEventError(err)
	}
	return events, total, nil
}

func (s *Service) Get(ctx context.Context, principal *authn.Principal, idOrSlug string) (*Event, error) {
	var (
		event *Event
		err   error
	)
	if _, parseErr := uuid.Parse(idOrSlug); parseErr == nil {
		event, err = s.repo.GetByID(ctx, idOrSlug)
	} else {
		event, err = s.repo.GetBySlug(ctx, idOrSlug)
	}
	if err != nil {
		return nil, mapEventError(err)
	}
	if !isPublicStatus(event.Status) && !canReadAll(principal) {
		return nil, apierr.NotFound("event")
	}
	return event, nil
}

func (s *Service) Create(ctx context.Context, principal *authn.Principal, req CreateEventRequest) (*Event, error) {
	event := &Event{
		ID:             uuid.NewString(),
		Title:          strings.TrimSpace(req.Title),
		Description:    strings.TrimSpace(req.Description),
		Type:           req.Type,
		Status:         StatusDraft,
		VenueName:      strings.TrimSpace(req.VenueName),
		VenueAddress:   strings.TrimSpace(req.VenueAddress),
		City:           strings.TrimSpace(req.City),
		Country:        strings.ToUpper(strings.TrimSpace(req.Country)),
		Capacity:       req.Capacity,
		PriceMin:       req.PriceMin,
		PriceMax:       req.PriceMax,
		Currency:       strings.ToUpper(strings.TrimSpace(req.Currency)),
		CoverImageURL:  strings.TrimSpace(req.CoverImageURL),
		OrganizerName:  strings.TrimSpace(req.OrganizerName),
		ContactEmail:   strings.TrimSpace(req.ContactEmail),
		ContactPhone:   strings.TrimSpace(req.ContactPhone),
		Tags:           cleanTags(req.Tags),
		Beneficiary:    strings.TrimSpace(req.Beneficiary),
		DonationTarget: req.DonationTarget,
		DonationRaised: req.DonationRaised,
		CreatedBy:      principal.UserID,
		CreatedByName:  principal.Name,
	}
	if req.StartsAt != nil {
		event.StartsAt = req.StartsAt.UTC()
	}
	if req.EndsAt != nil {
		endsAt := req.EndsAt.UTC()
		event.EndsAt = &endsAt
	}
	if event.Country == "" {
		event.Country = "ID"
	}
	if event.Currency == "" {
		event.Currency = "IDR"
	}

	if err := validateEvent(event); err != nil {
		return nil, err
	}

	autoSlug := strings.TrimSpace(req.Slug) == ""
	base := slugify.Make(req.Slug)
	if base == "" {
		base = "event"
	}
	if !autoSlug {
		exists, err := s.repo.SlugExists(ctx, base, "")
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, apierr.Conflict(ErrSlugTaken.Error())
		}
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if autoSlug && attempt > 0 {
			event.Slug = base + "-" + uuid.NewString()[:6]
		} else {
			event.Slug = base
		}
		lastErr = s.repo.Create(ctx, event)
		if lastErr == nil {
			return s.repo.GetByID(ctx, event.ID)
		}
		if !autoSlug || !errors.Is(lastErr, ErrSlugTaken) {
			break
		}
	}
	return nil, mapEventError(lastErr)
}

func (s *Service) Update(ctx context.Context, principal *authn.Principal, id string, req UpdateEventRequest) (*Event, error) {
	existing, err := s.getEditable(ctx, id)
	if err != nil {
		return nil, err
	}
	merged := *existing
	fields := updateFields{UpdatedBy: principal.UserID}

	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		fields.Title = &value
		merged.Title = value
	}
	if req.Slug != nil {
		value := slugify.Make(*req.Slug)
		if value == "" {
			return nil, apierr.Validation(map[string]string{"slug": "must contain at least one letter or number"})
		}
		if value != existing.Slug {
			exists, err := s.repo.SlugExists(ctx, value, id)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, apierr.Conflict(ErrSlugTaken.Error())
			}
		}
		fields.Slug = &value
		merged.Slug = value
	}
	if req.Description != nil {
		value := strings.TrimSpace(*req.Description)
		fields.Description = &value
		merged.Description = value
	}
	if req.Type != nil {
		value := strings.ToLower(strings.TrimSpace(*req.Type))
		fields.Type = &value
		merged.Type = value
	}
	if req.VenueName != nil {
		value := strings.TrimSpace(*req.VenueName)
		fields.VenueName = &value
		merged.VenueName = value
	}
	if req.VenueAddress != nil {
		value := strings.TrimSpace(*req.VenueAddress)
		fields.VenueAddress = &value
		merged.VenueAddress = value
	}
	if req.City != nil {
		value := strings.TrimSpace(*req.City)
		fields.City = &value
		merged.City = value
	}
	if req.Country != nil {
		value := strings.ToUpper(strings.TrimSpace(*req.Country))
		fields.Country = &value
		merged.Country = value
	}
	if req.StartsAt != nil {
		value := req.StartsAt.UTC()
		fields.StartsAt = &value
		merged.StartsAt = value
	}
	if req.EndsAt != nil {
		value := req.EndsAt.UTC()
		fields.EndsAt = &value
		merged.EndsAt = &value
	}
	if req.Capacity != nil {
		fields.Capacity = req.Capacity
		merged.Capacity = *req.Capacity
	}
	if req.PriceMin != nil {
		fields.PriceMin = req.PriceMin
		merged.PriceMin = *req.PriceMin
	}
	if req.PriceMax != nil {
		fields.PriceMax = req.PriceMax
		merged.PriceMax = *req.PriceMax
	}
	if req.Currency != nil {
		value := strings.ToUpper(strings.TrimSpace(*req.Currency))
		fields.Currency = &value
		merged.Currency = value
	}
	if req.CoverImageURL != nil {
		value := strings.TrimSpace(*req.CoverImageURL)
		fields.CoverImageURL = &value
		merged.CoverImageURL = value
	}
	if req.OrganizerName != nil {
		value := strings.TrimSpace(*req.OrganizerName)
		fields.OrganizerName = &value
		merged.OrganizerName = value
	}
	if req.ContactEmail != nil {
		value := strings.TrimSpace(*req.ContactEmail)
		fields.ContactEmail = &value
		merged.ContactEmail = value
	}
	if req.ContactPhone != nil {
		value := strings.TrimSpace(*req.ContactPhone)
		fields.ContactPhone = &value
		merged.ContactPhone = value
	}
	if req.Tags != nil {
		value := cleanTags(*req.Tags)
		fields.Tags = &value
		merged.Tags = value
	}
	if req.Beneficiary != nil {
		value := strings.TrimSpace(*req.Beneficiary)
		fields.Beneficiary = &value
		merged.Beneficiary = value
	}
	if req.DonationTarget != nil {
		fields.DonationTarget = req.DonationTarget
		merged.DonationTarget = *req.DonationTarget
	}
	if req.DonationRaised != nil {
		fields.DonationRaised = req.DonationRaised
		merged.DonationRaised = *req.DonationRaised
	}

	if err := validateEvent(&merged); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, id, fields); err != nil {
		return nil, mapEventError(err)
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) ChangeStatus(ctx context.Context, principal *authn.Principal, id string, req StatusRequest) (*Event, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapEventError(err)
	}
	next := req.Status
	if next == existing.Status {
		return nil, apierr.Conflict(fmt.Sprintf("event is already %s", next))
	}
	if !allowedTransitions[existing.Status][next] {
		return nil, apierr.Conflict(fmt.Sprintf("cannot move an event from %s to %s", existing.Status, next))
	}

	var publishedAt *time.Time
	if next == StatusPublished && existing.PublishedAt == nil {
		now := time.Now().UTC()
		publishedAt = &now
	}
	if err := s.repo.UpdateStatus(ctx, id, next, publishedAt, principal.UserID); err != nil {
		return nil, mapEventError(err)
	}
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return mapEventError(err)
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		return mapEventError(err)
	}
	return nil
}

func (s *Service) getEditable(ctx context.Context, id string) (*Event, error) {
	event, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, mapEventError(err)
	}
	if event.Status == StatusCompleted || event.Status == StatusCancelled {
		return nil, apierr.Conflict(ErrEventLocked.Error())
	}
	return event, nil
}

func validateEvent(e *Event) error {
	details := map[string]string{}

	if strings.TrimSpace(e.Title) == "" {
		details["title"] = "is required"
	}
	if e.StartsAt.IsZero() {
		details["starts_at"] = "is required"
	}
	if e.EndsAt != nil && e.EndsAt.Before(e.StartsAt) {
		details["ends_at"] = "must be after starts_at"
	}
	if e.PriceMax != 0 && e.PriceMax < e.PriceMin {
		details["price_max"] = "must be greater than or equal to price_min"
	}
	if !currencyPattern.MatchString(e.Currency) {
		details["currency"] = "must be a 3-letter ISO currency code"
	}
	if !countryPattern.MatchString(e.Country) {
		details["country"] = "must be a 2-letter ISO country code"
	}
	if e.Type == TypeCharity {
		if strings.TrimSpace(e.Beneficiary) == "" {
			details["beneficiary"] = "is required for charity events"
		}
		if e.DonationTarget <= 0 {
			details["donation_target"] = "must be greater than zero for charity events"
		}
	}

	if len(details) > 0 {
		return apierr.Validation(details)
	}
	return nil
}

func mapEventError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	switch {
	case errors.Is(err, ErrEventNotFound):
		return apierr.NotFound("event")
	case errors.Is(err, ErrSlugTaken):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrInvalidStatusTransition):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrEventLocked):
		return apierr.Conflict(err.Error())
	case errors.Is(err, ErrReviewNotFound):
		return apierr.NotFound("review")
	case errors.Is(err, ErrReviewAlreadyMade):
		return apierr.Conflict(err.Error())
	default:
		return err
	}
}

func canReadAll(principal *authn.Principal) bool {
	return principal != nil && principal.Has("event:read")
}

func isPublicStatus(status string) bool {
	for _, candidate := range publicStatuses {
		if candidate == status {
			return true
		}
	}
	return false
}

func validType(value string) bool {
	switch value {
	case TypeConcert, TypeCharity, TypeCommunity:
		return true
	}
	return false
}

func validStatus(value string) bool {
	switch value {
	case StatusDraft, StatusPublished, StatusOngoing, StatusCompleted, StatusCancelled:
		return true
	}
	return false
}

func cleanTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		out = append(out, tag)
	}
	return out
}
