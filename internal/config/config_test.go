package config

import (
	"slices"
	"testing"
)

func TestLoadCORSAllowedOriginsFromEnv(t *testing.T) {
	t.Setenv(
		"CORS_ALLOWED_ORIGINS",
		" https://app.example.com , https://admin.example.com ,",
	)

	cfg := Load()
	want := []string{"https://app.example.com", "https://admin.example.com"}

	if !slices.Equal(cfg.App.CORSAllowedOrigins, want) {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.App.CORSAllowedOrigins, want)
	}
}

func TestLoadDefaultCORSAllowedOrigins(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	cfg := Load()
	for _, origin := range []string{
		"http://localhost:5173",
		"http://localhost:4173",
		"https://*.vercel.app",
	} {
		if !slices.Contains(cfg.App.CORSAllowedOrigins, origin) {
			t.Errorf("CORSAllowedOrigins = %v, missing %q", cfg.App.CORSAllowedOrigins, origin)
		}
	}
}
