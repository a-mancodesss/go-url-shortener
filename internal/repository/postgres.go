package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/a-mancodesss/go-url-shortener/internal/model"
)

// PostgresRepo handles persistence for URL mappings.
type PostgresRepo struct {
	pool *pgxpool.Pool
}

func NewPostgresRepo(pool *pgxpool.Pool) *PostgresRepo {
	return &PostgresRepo{pool: pool}
}

// Create inserts a new URL mapping. Returns ErrAlreadyExists on duplicate short_code.
func (r *PostgresRepo) Create(ctx context.Context, u *model.URL) error {
	query := `
		INSERT INTO urls (id, short_code, long_url, created_at, expires_at, clicks)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := r.pool.Exec(ctx, query, u.ID, u.ShortCode, u.LongURL, u.CreatedAt, u.ExpiresAt, u.Clicks)
	if err != nil {
		// pgx error string contains "duplicate key" for unique violation
		if isDuplicateKey(err) {
			return model.ErrAlreadyExists
		}
		return err
	}
	return nil
}

// GetByShortCode fetches URL by short_code.
func (r *PostgresRepo) GetByShortCode(ctx context.Context, code string) (*model.URL, error) {
	query := `SELECT id, short_code, long_url, created_at, expires_at, clicks FROM urls WHERE short_code = $1`
	u := &model.URL{}
	err := r.pool.QueryRow(ctx, query, code).Scan(&u.ID, &u.ShortCode, &u.LongURL, &u.CreatedAt, &u.ExpiresAt, &u.Clicks)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set" {
			return nil, model.ErrNotFound
		}
		return nil, err
	}
	if u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt) {
		return nil, model.ErrExpired
	}
	return u, nil
}

// IncrementClicks asynchronously increments click counter (fire-and-forget ok).
func (r *PostgresRepo) IncrementClicks(ctx context.Context, code string) error {
	_, err := r.pool.Exec(ctx, `UPDATE urls SET clicks = clicks + 1 WHERE short_code = $1`, code)
	return err
}

// GetByLongURL checks if long_url already has a short code (for idempotency/dedup).
func (r *PostgresRepo) GetByLongURL(ctx context.Context, longURL string) (*model.URL, error) {
	query := `SELECT id, short_code, long_url, created_at, expires_at, clicks FROM urls WHERE long_url = $1 LIMIT 1`
	u := &model.URL{}
	err := r.pool.QueryRow(ctx, query, longURL).Scan(&u.ID, &u.ShortCode, &u.LongURL, &u.CreatedAt, &u.ExpiresAt, &u.Clicks)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, model.ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "duplicate key") || contains(s, "23505")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
