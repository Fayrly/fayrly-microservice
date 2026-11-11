package event

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

const eventSelect = `SELECT id::text AS id, slug, title, description, type, status,
	venue_name, venue_address, city, country, starts_at, ends_at, capacity,
	price_min, price_max, btrim(currency) AS currency, cover_image_url, organizer_name,
	contact_email, contact_phone, array_to_json(tags)::text AS tags_json, beneficiary,
	donation_target, donation_raised, created_by::text AS created_by, created_by_name,
	updated_by::text AS updated_by, published_at, created_at, updated_at,
	coalesce((SELECT count(*) FROM event_going g WHERE g.event_id = events.id), 0)::int AS going_count,
	coalesce((
		SELECT array_to_json(array_agg(names.display_name))
		FROM (
			SELECT g.display_name
			FROM event_going g
			WHERE g.event_id = events.id
			ORDER BY g.created_at, g.display_name
			LIMIT 3
		) names
	), '[]')::text AS going_json,
	coalesce((
		SELECT count(*) FROM event_reviews r
		WHERE r.event_id = events.id AND r.deleted_at IS NULL
	), 0)::int AS review_count,
	coalesce((
		SELECT round(avg(r.rating)::numeric, 1) FROM event_reviews r
		WHERE r.event_id = events.id AND r.deleted_at IS NULL
	), 0)::float8 AS rating_avg
	FROM events`

type eventRow struct {
	ID             string
	Slug           string
	Title          string
	Description    string
	Type           string
	Status         string
	VenueName      string
	VenueAddress   string
	City           string
	Country        string
	StartsAt       time.Time
	EndsAt         *time.Time
	Capacity       int
	PriceMin       int64
	PriceMax       int64
	Currency       string
	CoverImageURL  string
	OrganizerName  string
	ContactEmail   string
	ContactPhone   string
	TagsJSON       string
	Beneficiary    string
	DonationTarget int64
	DonationRaised int64
	CreatedBy      string
	CreatedByName  string
	UpdatedBy      *string
	PublishedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	GoingCount     int
	GoingJSON      string
	ReviewCount    int
	RatingAvg      float64
}

func (m eventRow) toDomain() (*Event, error) {
	e := &Event{
		ID:             m.ID,
		Slug:           m.Slug,
		Title:          m.Title,
		Description:    m.Description,
		Type:           m.Type,
		Status:         m.Status,
		VenueName:      m.VenueName,
		VenueAddress:   m.VenueAddress,
		City:           m.City,
		Country:        m.Country,
		StartsAt:       m.StartsAt,
		EndsAt:         m.EndsAt,
		Capacity:       m.Capacity,
		PriceMin:       m.PriceMin,
		PriceMax:       m.PriceMax,
		Currency:       m.Currency,
		CoverImageURL:  m.CoverImageURL,
		OrganizerName:  m.OrganizerName,
		ContactEmail:   m.ContactEmail,
		ContactPhone:   m.ContactPhone,
		Tags:           []string{},
		Beneficiary:    m.Beneficiary,
		DonationTarget: m.DonationTarget,
		DonationRaised: m.DonationRaised,
		CreatedBy:      m.CreatedBy,
		CreatedByName:  m.CreatedByName,
		UpdatedBy:      m.UpdatedBy,
		PublishedAt:    m.PublishedAt,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		GoingCount:     m.GoingCount,
		GoingNames:     []string{},
		ReviewCount:    m.ReviewCount,
		RatingAvg:      m.RatingAvg,
	}
	if strings.TrimSpace(m.GoingJSON) != "" {
		if err := json.Unmarshal([]byte(m.GoingJSON), &e.GoingNames); err != nil {
			return nil, err
		}
	}
	if e.GoingNames == nil {
		e.GoingNames = []string{}
	}
	if strings.TrimSpace(m.TagsJSON) != "" {
		if err := json.Unmarshal([]byte(m.TagsJSON), &e.Tags); err != nil {
			return nil, err
		}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	return e, nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Event, error) {
	return r.first(ctx, "id = ? AND deleted_at IS NULL", id)
}

func (r *Repository) GetBySlug(ctx context.Context, slug string) (*Event, error) {
	return r.first(ctx, "slug = ? AND deleted_at IS NULL", slug)
}

func (r *Repository) first(ctx context.Context, where string, args ...any) (*Event, error) {
	var row eventRow
	err := r.db.WithContext(ctx).Raw(eventSelect+" WHERE "+where, args...).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, ErrEventNotFound
	}
	return row.toDomain()
}

func (r *Repository) Create(ctx context.Context, e *Event) error {
	tags := e.Tags
	if tags == nil {
		tags = []string{}
	}
	err := r.db.WithContext(ctx).Exec(
		`INSERT INTO events (
			id, slug, title, description, type, status, venue_name, venue_address, city, country,
			starts_at, ends_at, capacity, price_min, price_max, currency, cover_image_url,
			organizer_name, contact_email, contact_phone, tags, beneficiary, donation_target,
			donation_raised, created_by, created_by_name
		) VALUES (
			?::uuid, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?::text[], ?, ?,
			?, ?::uuid, ?
		)`,
		e.ID, e.Slug, e.Title, e.Description, e.Type, e.Status, e.VenueName, e.VenueAddress, e.City, e.Country,
		e.StartsAt, e.EndsAt, e.Capacity, e.PriceMin, e.PriceMax, e.Currency, e.CoverImageURL,
		e.OrganizerName, e.ContactEmail, e.ContactPhone, textArrayLiteral(tags), e.Beneficiary, e.DonationTarget,
		e.DonationRaised, e.CreatedBy, e.CreatedByName).Error
	return mapEventUnique(err)
}

type updateFields struct {
	Slug           *string
	Title          *string
	Description    *string
	Type           *string
	VenueName      *string
	VenueAddress   *string
	City           *string
	Country        *string
	StartsAt       *time.Time
	EndsAt         *time.Time
	Capacity       *int
	PriceMin       *int64
	PriceMax       *int64
	Currency       *string
	CoverImageURL  *string
	OrganizerName  *string
	ContactEmail   *string
	ContactPhone   *string
	Tags           *[]string
	Beneficiary    *string
	DonationTarget *int64
	DonationRaised *int64
	UpdatedBy      string
}

func (r *Repository) Update(ctx context.Context, id string, f updateFields) error {
	var tags any
	if f.Tags != nil {
		tags = textArrayLiteral(*f.Tags)
	}
	err := r.db.WithContext(ctx).Exec(
		`UPDATE events SET
			slug            = COALESCE(?::text, slug),
			title           = COALESCE(?::text, title),
			description     = COALESCE(?::text, description),
			type            = COALESCE(?::text, type),
			venue_name      = COALESCE(?::text, venue_name),
			venue_address   = COALESCE(?::text, venue_address),
			city            = COALESCE(?::text, city),
			country         = COALESCE(?::text, country),
			starts_at       = COALESCE(?::timestamptz, starts_at),
			ends_at         = COALESCE(?::timestamptz, ends_at),
			capacity        = COALESCE(?::integer, capacity),
			price_min       = COALESCE(?::bigint, price_min),
			price_max       = COALESCE(?::bigint, price_max),
			currency        = COALESCE(?::text, currency),
			cover_image_url = COALESCE(?::text, cover_image_url),
			organizer_name  = COALESCE(?::text, organizer_name),
			contact_email   = COALESCE(?::text, contact_email),
			contact_phone   = COALESCE(?::text, contact_phone),
			tags            = COALESCE(?::text[], tags),
			beneficiary     = COALESCE(?::text, beneficiary),
			donation_target = COALESCE(?::bigint, donation_target),
			donation_raised = COALESCE(?::bigint, donation_raised),
			updated_by      = ?::uuid,
			updated_at      = now()
		 WHERE id = ? AND deleted_at IS NULL`,
		f.Slug, f.Title, f.Description, f.Type, f.VenueName, f.VenueAddress, f.City, f.Country,
		f.StartsAt, f.EndsAt, f.Capacity, f.PriceMin, f.PriceMax, f.Currency, f.CoverImageURL,
		f.OrganizerName, f.ContactEmail, f.ContactPhone, tags, f.Beneficiary, f.DonationTarget,
		f.DonationRaised, f.UpdatedBy, id).Error
	return mapEventUnique(err)
}

func (r *Repository) UpdateStatus(ctx context.Context, id, status string, publishedAt *time.Time, updatedBy string) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE events SET
			status       = ?,
			published_at = COALESCE(published_at, ?::timestamptz),
			updated_by   = ?::uuid,
			updated_at   = now()
		 WHERE id = ? AND deleted_at IS NULL`, status, publishedAt, updatedBy, id).Error
}

func (r *Repository) SoftDelete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(
		`UPDATE events SET deleted_at = now(), updated_at = now() WHERE id = ? AND deleted_at IS NULL`, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (r *Repository) SlugExists(ctx context.Context, slug, excludeID string) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).Raw(
		`SELECT EXISTS (
			SELECT 1 FROM events
			WHERE slug = ? AND (CAST(? AS text) = '' OR id <> CAST(NULLIF(CAST(? AS text), '') AS uuid))
		)`, slug, excludeID, excludeID).Scan(&exists).Error
	return exists, err
}

func (r *Repository) List(ctx context.Context, filter Filter, limit, offset int) ([]*Event, int, error) {
	var args []any
	where := []string{"deleted_at IS NULL"}

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		where = append(where, "(title ILIKE ? OR description ILIKE ? OR organizer_name ILIKE ?)")
		args = append(args, pattern, pattern, pattern)
	}
	if filter.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filter.Type)
	}
	if filter.City != "" {
		where = append(where, "city ILIKE ?")
		args = append(args, "%"+filter.City+"%")
	}
	if len(filter.Statuses) > 0 {
		where = append(where, "status IN ?")
		args = append(args, filter.Statuses)
	}
	if filter.CreatedBy != "" {
		where = append(where, "created_by = ?")
		args = append(args, filter.CreatedBy)
	}
	if filter.StartsFrom != nil {
		where = append(where, "starts_at >= ?")
		args = append(args, *filter.StartsFrom)
	}
	if filter.StartsTo != nil {
		where = append(where, "starts_at <= ?")
		args = append(args, *filter.StartsTo)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Table("events").Where(whereSQL, args...).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []eventRow
	err := r.db.WithContext(ctx).Raw(
		eventSelect+" WHERE "+whereSQL+" ORDER BY starts_at ASC, id LIMIT ? OFFSET ?",
		append(args, limit, offset)...).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	events := make([]*Event, 0, len(rows))
	for _, row := range rows {
		e, err := row.toDomain()
		if err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}
	return events, int(total), nil
}

// textArrayLiteral renders tags as a PostgreSQL array literal. Passing the
// literal as a single parameter keeps GORM from expanding the slice into a
// record tuple before the ::text[] cast.
func textArrayLiteral(values []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range value {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func mapEventUnique(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "slug") {
		return ErrSlugTaken
	}
	return err
}
