package config

import "os"

type Config struct {
	Server  ServerConfig
	Rakuten RakutenConfig
}

type ServerConfig struct {
	Port string
}

type RakutenConfig struct {
	BaseURL   string
	WSURL     string
	APIKey    string
	APISecret string
}

func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		Rakuten: RakutenConfig{
			BaseURL:   getEnv("RAKUTEN_API_BASE_URL", "https://exchange.rakuten-wallet.co.jp"),
			WSURL:     getEnv("RAKUTEN_WS_URL", "wss://exchange.rakuten-wallet.co.jp/ws"),
			APIKey:    getEnv("RAKUTEN_API_KEY", ""),
			APISecret: getEnv("RAKUTEN_API_SECRET", ""),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
