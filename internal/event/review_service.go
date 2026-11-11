package event

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/fayrly/backend/internal/platform/apierr"
	"github.com/fayrly/backend/internal/platform/authn"
	"github.com/fayrly/backend/internal/platform/httpx"
)

func (s *Service) ListReviews(ctx context.Context, page httpx.Page) ([]*Review, int, error) {
	reviews, total, err := s.repo.ListReviews(ctx, "", page.Size, page.Offset())
	if err != nil {
		return nil, 0, mapEventError(err)
	}
	return reviews, total, nil
}

func (s *Service) ListEventReviews(ctx context.Context, principal *authn.Principal, idOrSlug string, page httpx.Page) ([]*Review, ReviewSummary, error) {
	event, err := s.resolvePublicEvent(ctx, principal, idOrSlug)
	if err != nil {
		return nil, ReviewSummary{}, err
	}
	reviews, _, err := s.repo.ListReviews(ctx, event.ID, page.Size, page.Offset())
	if err != nil {
		return nil, ReviewSummary{}, mapEventError(err)
	}
	summary, err := s.repo.ReviewSummary(ctx, event.ID)
	if err != nil {
		return nil, ReviewSummary{}, mapEventError(err)
	}
	return reviews, summary, nil
}

func (s *Service) CreateReview(ctx context.Context, principal *authn.Principal, idOrSlug string, req CreateReviewRequest) (*Review, error) {
	event, err := s.resolvePublicEvent(ctx, principal, idOrSlug)
	if err != nil {
		return nil, err
	}
	if event.Status == StatusCompleted {
		return nil, apierr.Conflict("event sudah selesai dan tidak menerima ulasan baru")
	}

	body := strings.TrimSpace(req.Body)
	photo := strings.TrimSpace(req.PhotoURL)
	if body == "" {
		return nil, apierr.Validation(map[string]string{"body": "ulasan tidak boleh kosong"})
	}

	userName := strings.TrimSpace(principal.Name)
	if userName == "" {
		userName = "Pengguna Fayrly"
	}

	review := &Review{
		ID:        uuid.NewString(),
		EventID:   event.ID,
		EventSlug: event.Slug,
		UserName:  userName,
		UserID:    principal.UserID,
		Rating:    req.Rating,
		Body:      body,
		PhotoURL:  photo,
	}
	if err := s.repo.CreateReview(ctx, review); err != nil {
		return nil, mapEventError(err)
	}

	created, _, err := s.repo.ListReviews(ctx, event.ID, 50, 0)
	if err != nil {
		return review, nil
	}
	for _, candidate := range created {
		if candidate.ID == review.ID {
			return candidate, nil
		}
	}
	return review, nil
}

func (s *Service) MarkReviewHelpful(ctx context.Context, id string) (int, error) {
	if _, err := uuid.Parse(id); err != nil {
		return 0, apierr.NotFound("review")
	}
	count, err := s.repo.IncrementReviewHelpful(ctx, id)
	if err != nil {
		return 0, mapEventError(err)
	}
	return count, nil
}

func (s *Service) resolvePublicEvent(ctx context.Context, principal *authn.Principal, idOrSlug string) (*Event, error) {
	return s.Get(ctx, principal, idOrSlug)
}
