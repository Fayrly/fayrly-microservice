package event

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const reviewSelect = `SELECT r.id::text AS id, r.event_id::text AS event_id,
	e.slug AS event_slug, e.title AS event_title, e.cover_image_url AS event_cover,
	e.starts_at AS event_starts, r.user_id::text AS user_id, r.user_name,
	r.rating, r.body, r.photo_url, r.helpful_count, r.created_at
	FROM event_reviews r
	JOIN events e ON e.id = r.event_id
	WHERE r.deleted_at IS NULL AND e.deleted_at IS NULL
	AND e.status IN ('published', 'ongoing', 'completed')`

type reviewRow struct {
	ID           string
	EventID      string
	EventSlug    string
	EventTitle   string
	EventCover   string
	EventStarts  time.Time
	UserID       string
	UserName     string
	Rating       int
	Body         string
	PhotoURL     string
	HelpfulCount int
	CreatedAt    time.Time
}

func (m reviewRow) toDomain() *Review {
	return &Review{
		ID:           m.ID,
		EventID:      m.EventID,
		EventSlug:    m.EventSlug,
		EventTitle:   m.EventTitle,
		EventCover:   m.EventCover,
		EventStarts:  m.EventStarts,
		UserID:       m.UserID,
		UserName:     m.UserName,
		Rating:       m.Rating,
		Body:         m.Body,
		PhotoURL:     m.PhotoURL,
		HelpfulCount: m.HelpfulCount,
		CreatedAt:    m.CreatedAt,
	}
}

func (r *Repository) ListReviews(ctx context.Context, eventID string, limit, offset int) ([]*Review, int, error) {
	where := ""
	args := []any{}
	if eventID != "" {
		where = " AND r.event_id = ?"
		args = append(args, eventID)
	}

	var total int64
	if err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM event_reviews r
		 JOIN events e ON e.id = r.event_id
		 WHERE r.deleted_at IS NULL AND e.deleted_at IS NULL
		 AND e.status IN ('published', 'ongoing', 'completed')`+where, args...,
	).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []reviewRow
	query := reviewSelect + where + " ORDER BY r.created_at DESC, r.id LIMIT ? OFFSET ?"
	queryArgs := append(args, limit, offset)
	if err := r.db.WithContext(ctx).Raw(query, queryArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	reviews := make([]*Review, 0, len(rows))
	for _, row := range rows {
		reviews = append(reviews, row.toDomain())
	}
	return reviews, int(total), nil
}

func (r *Repository) ReviewSummary(ctx context.Context, eventID string) (ReviewSummary, error) {
	var summary ReviewSummary
	err := r.db.WithContext(ctx).Raw(
		`SELECT count(*)::int AS count, coalesce(round(avg(rating)::numeric, 1), 0)::float8 AS average
		 FROM event_reviews WHERE event_id = ? AND deleted_at IS NULL`, eventID,
	).Scan(&summary).Error
	return summary, err
}

func (r *Repository) CreateReview(ctx context.Context, review *Review) error {
	err := r.db.WithContext(ctx).Exec(
		`INSERT INTO event_reviews (id, event_id, user_id, user_name, rating, body, photo_url)
		 VALUES (?::uuid, ?::uuid, ?::uuid, ?, ?, ?, ?)`,
		review.ID, review.EventID, review.UserID, review.UserName, review.Rating,
		review.Body, review.PhotoURL,
	).Error
	return mapReviewUnique(err)
}

func (r *Repository) IncrementReviewHelpful(ctx context.Context, id string) (int, error) {
	var count int
	result := r.db.WithContext(ctx).Raw(
		`UPDATE event_reviews SET helpful_count = helpful_count + 1, updated_at = now()
		 WHERE id = ? AND deleted_at IS NULL RETURNING helpful_count`, id,
	).Scan(&count)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, ErrReviewNotFound
	}
	return count, nil
}

func mapReviewUnique(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		strings.Contains(pgErr.ConstraintName, "event_reviews_one_per_user") {
		return ErrReviewAlreadyMade
	}
	return err
}
