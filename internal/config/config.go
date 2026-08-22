package config

import (
	"os"
	"time"
)

type Config struct {
	DatabaseURL string
	Port        string
	JWTSecret   string
	JWTExpiry   time.Duration
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	expiry := 24 * time.Hour
	if v := os.Getenv("JWT_EXPIRY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			expiry = d
		}
	}

	return &Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        port,
		JWTSecret:   os.Getenv("JWT_SECRET"),
		JWTExpiry:   expiry,
	}
}
