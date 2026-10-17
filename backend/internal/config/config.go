package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ServerAddr          string
	DatabaseURL         string
	CORSAllowedOrigins  []string
	TelegramAPIKey      string
	TelegramMediaDir    string
	GeoapifyAPIKey      string
	GeoapifyDailyLimit  int
	GeoapifyMinuteLimit int
	GeoapifyGlobalLimit int
}

func Load() Config {
	return Config{
		ServerAddr:          env("SERVER_ADDR", ":8080"),
		DatabaseURL:         env("DATABASE_URL", ""),
		CORSAllowedOrigins:  csvEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),
		TelegramAPIKey:      strings.TrimSpace(os.Getenv("TELEGRAM_API_KEY")),
		TelegramMediaDir:    env("TELEGRAM_MEDIA_DIR", "./data/media"),
		GeoapifyAPIKey:      strings.TrimSpace(os.Getenv("GEOAPIFY_API_KEY")),
		GeoapifyDailyLimit:  intEnv("GEOAPIFY_COMMUTE_DAILY_LIMIT", 50, 1, 3000),
		GeoapifyMinuteLimit: intEnv("GEOAPIFY_COMMUTE_PER_MINUTE_LIMIT", 10, 1, 600),
		GeoapifyGlobalLimit: intEnv("GEOAPIFY_DAILY_LIMIT", 2500, 1, 3000),
	}
}

func intEnv(key string, fallback, min, max int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < min || parsed > max {
		return fallback
	}
	return parsed
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func csvEnv(key, fallback string) []string {
	raw := env(key, fallback)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
