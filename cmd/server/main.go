package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/example/url-shortener/internal/cache"
	"github.com/example/url-shortener/internal/config"
	"github.com/example/url-shortener/internal/handler"
	"github.com/example/url-shortener/internal/middleware"
	"github.com/example/url-shortener/internal/repository"
	"github.com/example/url-shortener/internal/service"
	"github.com/example/url-shortener/internal/util"
)

func main() {
	// Load .env if present (ignore error in production where env is injected)
	_ = godotenv.Load()

	cfg := config.Load()

	// Gin mode
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Snowflake node ID from env (k8s StatefulSet ordinal or POD_ID)
	nodeID := int64(envInt("SNOWFLAKE_NODE_ID", 1))
	gen, err := util.NewSnowflake(nodeID)
	if err != nil {
		log.Fatalf("snowflake init: %v", err)
	}

	// Postgres
	pool, err := pgxpool.New(context.Background(), cfg.Postgres.DSN())
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer pool.Close()

	// Verify DB
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("postgres ping: %v", err)
	}
	cancel()
	log.Println("✓ PostgreSQL connected")

	// Run migrations (simple: read and exec up.sql if table missing)
	if err := runMigrations(pool); err != nil {
		log.Printf("migration warning: %v", err)
	}

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redis ping: %v", err)
	}
	log.Println("✓ Redis connected")
	redisCache := cache.NewRedisCache(rdb, cfg.Redis.TTL)

	// Wiring
	repo := repository.NewPostgresRepo(pool)
	svc := service.NewURLService(repo, redisCache, gen, cfg)
	h := handler.NewURLHandler(svc)

	// Router
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())

	// Global rate limiter: 100 req/min per IP for API routes; looser for redirects
	// Fine-tune per-route in production (e.g., stricter for POST /shorten)
	apiLimiter := middleware.RateLimiter(redisCache, 60, time.Minute)
	redirectLimiter := middleware.RateLimiter(redisCache, 300, time.Minute)

	r.GET("/health", handler.Health)
	r.GET("/:code", redirectLimiter, h.Redirect)

	api := r.Group("/api/v1")
	api.Use(apiLimiter)
	{
		api.POST("/shorten", h.Shorten)
		api.GET("/stats/:code", h.Stats)
	}

	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown
	go func() {
		log.Printf("🚀 Server listening on :%s (env=%s)", cfg.Server.Port, cfg.App.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if err := srv.Shutdown(ctx2); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("Server exited")
}

func runMigrations(pool *pgxpool.Pool) error {
	// Minimal embedded migration — for production use golang-migrate or goose
	// This ensures table exists without external tooling.
	query := `
	CREATE TABLE IF NOT EXISTS urls (
		id BIGINT PRIMARY KEY,
		short_code VARCHAR(20) NOT NULL UNIQUE,
		long_url TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		expires_at TIMESTAMPTZ,
		clicks BIGINT NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_urls_long_url ON urls(long_url);
	CREATE INDEX IF NOT EXISTS idx_urls_expires_at ON urls(expires_at) WHERE expires_at IS NOT NULL;
	`
	_, err := pool.Exec(context.Background(), query)
	return err
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
