package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
// Values are loaded from environment variables with sensible defaults for local dev.
type Config struct {
	App      AppConfig
	Server   ServerConfig
	Postgres PostgresConfig
	Redis    RedisConfig
}

type AppConfig struct {
	Env             string // development | production
	BaseURL         string // e.g. https://amanly.com
	ShortCodeLength int
}

type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	MaxConns int32
	MinConns int32
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	TTL      time.Duration // default cache TTL for URL mappings
}

// DSN returns PostgreSQL connection string for pgx.
func (p PostgresConfig) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.DBName, p.SSLMode)
}

// Load reads configuration from environment variables.
func Load() *Config {
	return &Config{
		App: AppConfig{
			Env:             env("APP_ENV", "development"),
			BaseURL:         env("BASE_URL", "https://amanly.com"),
			ShortCodeLength: envInt("SHORT_CODE_LENGTH", 7),
		},
		Server: ServerConfig{
			Port:         env("PORT", "8080"),
			ReadTimeout:  envDuration("SERVER_READ_TIMEOUT", 5*time.Second),
			WriteTimeout: envDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
			IdleTimeout:  envDuration("SERVER_IDLE_TIMEOUT", 60*time.Second),
		},
		Postgres: PostgresConfig{
			Host:     env("POSTGRES_HOST", "localhost"),
			Port:     env("POSTGRES_PORT", "5432"),
			User:     env("POSTGRES_USER", "postgres"),
			Password: env("POSTGRES_PASSWORD", "postgres"),
			DBName:   env("POSTGRES_DB", "urlshortener"),
			SSLMode:  env("POSTGRES_SSLMODE", "disable"),
			MaxConns: int32(envInt("POSTGRES_MAX_CONNS", 25)),
			MinConns: int32(envInt("POSTGRES_MIN_CONNS", 5)),
		},
		Redis: RedisConfig{
			Addr:     env("REDIS_ADDR", "localhost:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
			TTL:      envDuration("REDIS_TTL", 24*time.Hour),
		},
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
