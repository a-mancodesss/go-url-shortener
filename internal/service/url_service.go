package service

import (
	"context"
	"time"

	"github.com/example/url-shortener/internal/cache"
	"github.com/example/url-shortener/internal/config"
	"github.com/example/url-shortener/internal/model"
	"github.com/example/url-shortener/internal/repository"
	"github.com/example/url-shortener/internal/util"
)

// URLService orchestrates shortening, cache, and persistence.
type URLService struct {
	repo      *repository.PostgresRepo
	cache     *cache.RedisCache
	generator *util.Snowflake
	config    *config.Config
}

func NewURLService(repo *repository.PostgresRepo, cache *cache.RedisCache, gen *util.Snowflake, cfg *config.Config) *URLService {
	return &URLService{repo: repo, cache: cache, generator: gen, config: cfg}
}

// Shorten creates a short code for longURL.
// Flow:
//  1. Validate URL
//  2. If custom alias provided, try to use it (collision -> error)
//  3. Otherwise generate Snowflake ID -> Base62 -> short code
//  4. Persist to Postgres
//  5. Cache in Redis (write-through)
func (s *URLService) Shorten(ctx context.Context, req model.CreateURLRequest) (*model.CreateURLResponse, error) {
	if !util.IsValidURL(req.LongURL) {
		return nil, model.ErrInvalidURL
	}

	var shortCode string
	var id int64

	if req.CustomAlias != "" {
		if !util.IsValidAlias(req.CustomAlias) {
			return nil, model.ErrInvalidURL
		}
		shortCode = req.CustomAlias
		id = s.generator.NextID() // still generate ID for PK even for custom alias
	} else {
		id = s.generator.NextID()
		code := util.EncodeBase62(id)
		// Ensure minimum length (pad) for aesthetics/consistency
		if len(code) < s.config.App.ShortCodeLength {
			code = util.PadLeft(code, s.config.App.ShortCodeLength)
		}
		shortCode = code
	}

	now := time.Now()
	url := &model.URL{
		ID:        id,
		ShortCode: shortCode,
		LongURL:   req.LongURL,
		CreatedAt: now,
		ExpiresAt: req.ExpiresAt,
		Clicks:    0,
	}

	if err := s.repo.Create(ctx, url); err != nil {
		return nil, err
	}

	// Cache asynchronously — best effort, don't fail request if Redis down
	ttl := s.config.Redis.TTL
	if url.ExpiresAt != nil {
		remaining := time.Until(*url.ExpiresAt)
		if remaining > 0 && remaining < ttl {
			ttl = remaining
		}
	}
	_ = s.cache.SetWithTTL(context.Background(), shortCode, url.LongURL, ttl)

	shortURL := s.config.App.BaseURL + "/" + shortCode
	return &model.CreateURLResponse{
		ShortCode: shortCode,
		ShortURL:  shortURL,
		LongURL:   url.LongURL,
		ExpiresAt: url.ExpiresAt,
	}, nil
}

// Resolve returns long URL for short code.
// Cache-aside pattern:
//  1. Try Redis cache
//  2. On miss, fetch Postgres
//  3. Populate cache
//  4. Increment clicks async (fire-and-forget)
func (s *URLService) Resolve(ctx context.Context, code string) (string, error) {
	// 1. Cache hit?
	if longURL, err := s.cache.Get(ctx, code); err == nil {
		// Async click increment
		go func() {
			_ = s.repo.IncrementClicks(context.Background(), code)
		}()
		return longURL, nil
	}

	// 2. Cache miss -> DB
	u, err := s.repo.GetByShortCode(ctx, code)
	if err != nil {
		return "", err
	}

	// 3. Populate cache
	ttl := s.config.Redis.TTL
	if u.ExpiresAt != nil {
		remaining := time.Until(*u.ExpiresAt)
		if remaining <= 0 {
			return "", model.ErrExpired
		}
		if remaining < ttl {
			ttl = remaining
		}
	}
	_ = s.cache.SetWithTTL(context.Background(), code, u.LongURL, ttl)

	// 4. Increment clicks async
	go func() {
		_ = s.repo.IncrementClicks(context.Background(), code)
	}()

	return u.LongURL, nil
}

// Stats returns URL info (useful for /api/v1/stats/:code)
func (s *URLService) Stats(ctx context.Context, code string) (*model.URL, error) {
	return s.repo.GetByShortCode(ctx, code)
}
