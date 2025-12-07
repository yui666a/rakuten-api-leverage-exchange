package config

import "os"

// Config holds application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	External ExternalAPIConfig
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
}

type ExternalAPIConfig struct {
	BaseURL string
	APIKey  string
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			DBName:   getEnv("DB_NAME", "mydb"),
		},
		External: ExternalAPIConfig{
			BaseURL: getEnv("EXTERNAL_API_URL", ""),
			APIKey:  getEnv("EXTERNAL_API_KEY", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
