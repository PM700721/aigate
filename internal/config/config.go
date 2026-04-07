package config

import (
	"fmt"
	"os"
	"strconv"
)

const Version = "0.1.0"

type Config struct {
	Host     string
	Port     int
	APIKey   string // Proxy API key for client auth
	LogLevel string

	// Kiro credentials
	KiroRegion   string
	RefreshToken string
	ProfileARN   string
	CredsFile    string
	CLIDBFile    string

	// Proxy/VPN
	ProxyURL string

	// Streaming
	FirstTokenTimeout    float64
	FirstTokenMaxRetries int
	StreamReadTimeout    float64
}

func Load() (*Config, error) {
	port, _ := strconv.Atoi(getEnv("PORT", "8000"))

	apiKey := getEnv("API_KEY", "")
	if apiKey == "" {
		return nil, fmt.Errorf("API_KEY is required — set it in .env or environment")
	}

	return &Config{
		Host:     getEnv("HOST", "0.0.0.0"),
		Port:     port,
		APIKey:   apiKey,
		LogLevel: getEnv("LOG_LEVEL", "info"),

		KiroRegion:   getEnv("KIRO_REGION", "us-east-1"),
		RefreshToken: getEnv("REFRESH_TOKEN", ""),
		ProfileARN:   getEnv("PROFILE_ARN", ""),
		CredsFile:    getEnv("KIRO_CREDS_FILE", ""),
		CLIDBFile:    getEnv("KIRO_CLI_DB_FILE", ""),

		ProxyURL: getEnv("VPN_PROXY_URL", ""),

		FirstTokenTimeout:    getEnvFloat("FIRST_TOKEN_TIMEOUT", 15),
		FirstTokenMaxRetries: getEnvInt("FIRST_TOKEN_MAX_RETRIES", 3),
		StreamReadTimeout:    getEnvFloat("STREAMING_READ_TIMEOUT", 300),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
