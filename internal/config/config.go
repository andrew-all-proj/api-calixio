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
	AWS struct {
		Bucket          string
		Region          string
		AccessKeyID     string
		SecretAccessKey string
		Endpoint        string
		PathStyle       bool
		PublicURL       string
		PresignTTL      time.Duration
		MaxUploadBytes  int64
		AllowedMIMEs    []string
	}
	Transcoding struct {
		Enabled       bool
		FFmpegPath    string
		FFprobePath   string
		WorkDir       string
		HLSSegmentSec int
		QueueSize     int
		JobTimeout    time.Duration
	}
	MediaPlayback struct {
		SignedTTL time.Duration
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

	cfg.AWS.Bucket = getenv("AWS_S3_BUCKET", "")
	cfg.AWS.Region = getenv("AWS_REGION", "us-east-1")
	cfg.AWS.AccessKeyID = getenv("AWS_ACCESS_KEY_ID", "")
	cfg.AWS.SecretAccessKey = getenv("AWS_SECRET_ACCESS_KEY", "")
	cfg.AWS.Endpoint = getenv("AWS_S3_ENDPOINT", "")
	cfg.AWS.PathStyle = getenv("AWS_S3_PATH_STYLE", "false") == "true"
	cfg.AWS.PublicURL = getenv("AWS_S3_PUBLIC_URL", "")
	cfg.AWS.PresignTTL = getenvDuration("AWS_S3_PRESIGN_TTL", 15*time.Minute)
	cfg.AWS.MaxUploadBytes = getenvInt64("AWS_S3_MAX_UPLOAD_BYTES", 10*1024*1024*1024)
	cfg.AWS.AllowedMIMEs = getenvCSV("AWS_S3_ALLOWED_MIME_TYPES", []string{
		"video/mp4",
		"video/webm",
		"video/quicktime",
		"video/x-msvideo",
		"video/matroska",
		"video/x-matroska",
	})

	cfg.Transcoding.Enabled = getenv("TRANSCODER_ENABLED", "false") == "true"
	cfg.Transcoding.FFmpegPath = getenv("TRANSCODER_FFMPEG_PATH", "ffmpeg")
	cfg.Transcoding.FFprobePath = getenv("TRANSCODER_FFPROBE_PATH", "ffprobe")
	cfg.Transcoding.WorkDir = getenv("TRANSCODER_WORK_DIR", "")
	cfg.Transcoding.HLSSegmentSec = getenvInt("TRANSCODER_HLS_SEGMENT_SEC", 6)
	cfg.Transcoding.QueueSize = getenvInt("TRANSCODER_QUEUE_SIZE", 32)
	cfg.Transcoding.JobTimeout = 4 * time.Hour
	cfg.MediaPlayback.SignedTTL = getenvDuration("MEDIA_PLAYBACK_SIGNED_TTL", 3*time.Hour)

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

func getenvInt64(key string, fallback int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.ParseInt(val, 10, 64)
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
