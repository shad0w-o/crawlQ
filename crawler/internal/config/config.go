package config

import (
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string
	UserAgent   string
	MaxPages    int
	MaxDepth    int
	Concurrency int
	DelayMs     int
	APIPort     string
	AllowedOrigins string
}

func Load() Config {
	return Config{
		DatabaseURL: envOr("DATABASE_URL", "postgres://crawler:crawler@localhost:5432/crawler?sslmode=disable"),
		UserAgent:   envOr("CRAWLER_USER_AGENT", "CrawlerBot/1.0 (+https://example.com/bot)"),
		MaxPages:    envInt("CRAWLER_MAX_PAGES", 1000),
		MaxDepth:    envInt("CRAWLER_MAX_DEPTH", 2),
		Concurrency: envInt("CRAWLER_CONCURRENCY", 10),
		DelayMs:     envInt("CRAWLER_DELAY_MS", 500),
		APIPort:     envOr("API_PORT", "8080"),
		AllowedOrigins: envOr("CORS_ALLOWED_ORIGINS", ""),
	}
}

func envOr(key, fallback string) string {
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
