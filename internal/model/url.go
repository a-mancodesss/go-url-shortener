package model

import (
	"errors"
	"time"
)

// URL represents a shortened URL mapping.
type URL struct {
	ID        int64     `db:"id" json:"id"`
	ShortCode string    `db:"short_code" json:"short_code"`
	LongURL   string    `db:"long_url" json:"long_url"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	ExpiresAt *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	Clicks    int64     `db:"clicks" json:"clicks"`
}

// CreateURLRequest is the payload for POST /api/v1/shorten
type CreateURLRequest struct {
	LongURL   string `json:"long_url" binding:"required,url"`
	CustomAlias string `json:"custom_alias,omitempty" binding:"omitempty,alphanum,min=3,max=20"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateURLResponse is returned after successful shortening.
type CreateURLResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
	LongURL   string `json:"long_url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

var (
	ErrNotFound      = errors.New("short code not found")
	ErrAlreadyExists = errors.New("custom alias already exists")
	ErrExpired       = errors.New("short URL has expired")
	ErrInvalidURL    = errors.New("invalid URL")
)
