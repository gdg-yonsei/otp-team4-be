package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port           string
	DBDriver       string // sqlite | postgres
	DatabaseURL    string
	PublicBaseURL  string
	CookieSecure   bool
	SessionTTL     time.Duration
	MaxUploadBytes int64
	AllowedOrigins []string

	AgentURL     string
	AgentToken   string
	AgentTimeout time.Duration
	JobTimeout   time.Duration
}

func Load() Config {
	return Config{
		Port:           env("PORT", "8080"),
		DBDriver:       env("DB_DRIVER", "sqlite"),
		DatabaseURL:    env("DATABASE_URL", "katalk-wiki.db"),
		PublicBaseURL:  strings.TrimRight(env("PUBLIC_BASE_URL", "https://katalk.wiki"), "/"),
		CookieSecure:   envBool("COOKIE_SECURE", false),
		SessionTTL:     envDuration("SESSION_TTL", 7*24*time.Hour),
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_MB", 50)) << 20,
		AllowedOrigins: splitNonEmpty(env("ALLOWED_ORIGINS", "")),

		AgentURL:     env("AGENT_URL", ""),
		AgentToken:   env("AGENT_TOKEN", ""),
		AgentTimeout: envDuration("AGENT_TIMEOUT", 30*time.Second),
		JobTimeout:   envDuration("JOB_TIMEOUT", 60*time.Minute),
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
