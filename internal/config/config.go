package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	MinIO    MinIOConfig
	Auth     AuthConfig
	Chunks   ChunkConfig
}

type ServerConfig struct {
	Host string
	Port int
}

func (s ServerConfig) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

type DatabaseConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	MaxConns int
}

func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

type MinIOConfig struct {
	Endpoint    string
	AccessKey   string
	SecretKey   string
	UseSSL      bool
	ChunkBucket string
}

type AuthConfig struct {
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type ChunkConfig struct {
	MaxSize             int64
	UploadConcurrency   int
	DownloadConcurrency int
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host: envStr("STRATA_HOST", "0.0.0.0"),
			Port: envInt("STRATA_PORT", 8080),
		},
		Database: DatabaseConfig{
			Host:     envStr("STRATA_DB_HOST", "localhost"),
			Port:     envInt("STRATA_DB_PORT", 5432),
			Name:     envStr("STRATA_DB_NAME", "strata"),
			User:     envStr("STRATA_DB_USER", "strata"),
			Password: envStr("STRATA_DB_PASSWORD", ""),
			MaxConns: envInt("STRATA_DB_MAX_CONNS", 25),
		},
		MinIO: MinIOConfig{
			Endpoint:    envStr("STRATA_MINIO_ENDPOINT", "localhost:9000"),
			AccessKey:   envStr("STRATA_MINIO_ACCESS_KEY", "minioadmin"),
			SecretKey:   envStr("STRATA_MINIO_SECRET_KEY", "minioadmin"),
			UseSSL:      envBool("STRATA_MINIO_USE_SSL", false),
			ChunkBucket: envStr("STRATA_MINIO_CHUNK_BUCKET", "chunks"),
		},
		Auth: AuthConfig{
			JWTSecret:       envStr("STRATA_JWT_SECRET", ""),
			AccessTokenTTL:  envDuration("STRATA_ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL: envDuration("STRATA_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		},
		Chunks: ChunkConfig{
			MaxSize:             envInt64("STRATA_MAX_CHUNK_SIZE", 4*1024*1024), // 4MB
			UploadConcurrency:   envInt("STRATA_UPLOAD_CONCURRENCY", 32),
			DownloadConcurrency: envInt("STRATA_DOWNLOAD_CONCURRENCY", 64),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.Database.Password == "" {
		return fmt.Errorf("STRATA_DB_PASSWORD is required")
	}
	if c.Auth.JWTSecret == "" {
		return fmt.Errorf("STRATA_JWT_SECRET is required (generate with: openssl rand -hex 32)")
	}
	if len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("STRATA_JWT_SECRET must be at least 32 characters")
	}
	return nil
}

// Helper functions for reading environment variables with defaults.

func envStr(key, fallback string) string {
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

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
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
