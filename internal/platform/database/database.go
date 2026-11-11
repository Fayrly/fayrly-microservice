// Package database provides Postgres connection helpers, a GORM-backed
// transaction helper and a tiny embedded migration runner. Every service owns
// its own database.
package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Connect opens a GORM connection backed by pgx and waits for Postgres to accept
// connections. The retry loop makes container start ordering irrelevant.
func Connect(ctx context.Context, url string, logger *slog.Logger) (*gorm.DB, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("database url is empty")
	}

	db, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger:                 newSlogLogger(logger),
		SkipDefaultTransaction: true,
		TranslateError:         false,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	const attempts = 30
	for i := 1; ; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = sqlDB.PingContext(pingCtx)
		cancel()
		if err == nil {
			return db, nil
		}
		if i >= attempts {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("database not reachable after %d attempts: %w", attempts, err)
		}
		logger.Warn("database not ready, retrying", "attempt", i, "error", err)
		select {
		case <-ctx.Done():
			_ = sqlDB.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Ping checks the underlying connection pool and is used by readiness probes.
func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close releases the underlying connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate applies every *.sql file in dir (embedded) exactly once, in lexical
// order. Each migration runs in its own transaction guarded by an advisory lock
// so that multiple replicas can boot concurrently.
//
// Schema changes stay in plain SQL: partial unique indexes, check constraints
// and RBAC seeds are part of the contract and are not expressed through GORM
// AutoMigrate.
func Migrate(ctx context.Context, db *gorm.DB, files fs.FS, dir string, logger *slog.Logger) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("access sql connection pool: %w", err)
	}

	if _, err := sqlDB.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if err := applyMigration(ctx, sqlDB, files, dir, name, logger); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, sqlDB *sql.DB, files fs.FS, dir, name string, logger *slog.Logger) error {
	body, err := fs.ReadFile(files, path.Join(dir, name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	// 0x66617972 is "fayr" in ASCII, a stable lock key for this schema.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x66617972)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check migration %s: %w", name, err)
	}
	if exists {
		return nil
	}

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	logger.Info("migration applied", "version", name)
	return nil
}

// WithTx runs fn inside a GORM transaction, rolling back on error or panic.
func WithTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(fn)
}

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

// JSON is a jsonb column value. It implements sql.Scanner and driver.Valuer so
// it can be used directly in GORM models.
type JSON []byte

func (j *JSON) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSON", value)
	}
	return nil
}

func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "{}", nil
	}
	return string(j), nil
}

func (JSON) GormDataType() string { return "jsonb" }

// slogLogger routes GORM diagnostics through the service's structured logger.
// Only failures and slow queries are emitted; routine queries stay quiet.
type slogLogger struct {
	logger        *slog.Logger
	slowThreshold time.Duration
}

func newSlogLogger(logger *slog.Logger) gormlogger.Interface {
	return &slogLogger{logger: logger, slowThreshold: 500 * time.Millisecond}
}

func (l *slogLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *slogLogger) Info(ctx context.Context, msg string, data ...any) {
	l.logger.InfoContext(ctx, fmt.Sprintf(msg, data...))
}

func (l *slogLogger) Warn(ctx context.Context, msg string, data ...any) {
	l.logger.WarnContext(ctx, fmt.Sprintf(msg, data...))
}

func (l *slogLogger) Error(ctx context.Context, msg string, data ...any) {
	l.logger.ErrorContext(ctx, fmt.Sprintf(msg, data...))
}

func (l *slogLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	attrs := []any{
		"elapsed_ms", elapsed.Milliseconds(),
		"rows", rows,
		"sql", sql,
	}

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		l.logger.WarnContext(ctx, "database query failed", append(attrs, "error", err)...)
	case elapsed > l.slowThreshold:
		l.logger.WarnContext(ctx, "slow database query", attrs...)
	}
}
