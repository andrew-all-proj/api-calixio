package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string
	HTTPAddr    string
	DatabaseURL string
	RedisAddr   string
	RedisDB     int
	JWTSecret   string
	CORSOrigins []string
	AccessTTL   time.Duration
	RefreshTTL  time.Duration

	LiveKit struct {
		APIKey          string
		APISecret       string
		Host            string
		WebhookSecret   string
		RoomAutoTimeout time.Duration
		TokenTTL        time.Duration
	}
}

func Load() (*Config, error) {
	cfg := &Config{}

	if err := loadDotEnv(".env"); err != nil {
		return nil, err
	}

	cfg.Env = getenv("APP_ENV", "dev")
	cfg.HTTPAddr = getenv("HTTP_ADDR", ":8080")
	cfg.DatabaseURL = getenv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/livekit?sslmode=disable")
	cfg.RedisAddr = getenv("REDIS_ADDR", "redis:6379")
	cfg.RedisDB = getenvInt("REDIS_DB", 0)
	cfg.JWTSecret = getenv("JWT_SECRET", "change-me")
	cfg.CORSOrigins = getenvCSV("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173", "http://127.0.0.1:5173"})
	cfg.AccessTTL = getenvDuration("ACCESS_TOKEN_TTL", 10*time.Minute)
	cfg.RefreshTTL = getenvDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour)

	cfg.LiveKit.APIKey = getenv("LIVEKIT_API_KEY", "devkey")
	cfg.LiveKit.APISecret = getenv("LIVEKIT_API_SECRET", "devsecret")
	cfg.LiveKit.Host = getenv("LIVEKIT_HOST", "http://livekit:7880")
	cfg.LiveKit.WebhookSecret = getenv("LIVEKIT_WEBHOOK_SECRET", "devwebhook")
	cfg.LiveKit.RoomAutoTimeout = getenvDuration("LIVEKIT_ROOM_AUTO_TIMEOUT", 2*time.Hour)
	cfg.LiveKit.TokenTTL = getenvDuration("LIVEKIT_TOKEN_TTL", 2*time.Hour)

	if cfg.JWTSecret == "change-me" {
		return nil, fmt.Errorf("JWT_SECRET must be set")
	}

	return cfg, nil
}

func getenvCSV(key string, fallback []string) []string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	parts := strings.Split(val, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func loadDotEnv(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return scanner.Err()
}

func getenv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return fallback
	}
	return d
}
