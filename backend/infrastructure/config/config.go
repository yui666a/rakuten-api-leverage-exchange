package config

import (
	"os"
)

// Config holds application configuration
type Config struct {
	Server   ServerConfig
	Rakuten  RakutenConfig
	Database DatabaseConfig
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Port string
	Host string
}

// RakutenConfig holds Rakuten API configuration
type RakutenConfig struct {
	BaseURL string
	APIKey  string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type string
	DSN  string
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
		},
		Rakuten: RakutenConfig{
			BaseURL: getEnv("RAKUTEN_BASE_URL", "https://api.rakuten-sec.co.jp"),
			APIKey:  getEnv("RAKUTEN_API_KEY", ""),
		},
		Database: DatabaseConfig{
			Type: getEnv("DB_TYPE", "memory"),
			DSN:  getEnv("DB_DSN", ""),
		},
	}
}

// getEnv retrieves an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
