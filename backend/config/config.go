package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server   ServerConfig
	Rakuten  RakutenConfig
	Database DatabaseConfig
	Risk     RiskConfig
	LLM      LLMConfig
}

type LLMConfig struct {
	APIKey      string
	Model       string
	MaxTokens   int64
	CacheTTLMin int
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Path string
}

type RiskConfig struct {
	MaxPositionAmount float64
	MaxDailyLoss      float64
	StopLossPercent   float64
	InitialCapital    float64
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
		Database: DatabaseConfig{
			Path: getEnv("DATABASE_PATH", "data/trading.db"),
		},
		Risk: RiskConfig{
			MaxPositionAmount: getEnvFloat("RISK_MAX_POSITION_AMOUNT", 5000),
			MaxDailyLoss:      getEnvFloat("RISK_MAX_DAILY_LOSS", 5000),
			StopLossPercent:   getEnvFloat("RISK_STOP_LOSS_PERCENT", 5),
			InitialCapital:    getEnvFloat("RISK_INITIAL_CAPITAL", 10000),
		},
		Rakuten: RakutenConfig{
			BaseURL:   getEnv("RAKUTEN_API_BASE_URL", "https://exchange.rakuten-wallet.co.jp"),
			WSURL:     getEnv("RAKUTEN_WS_URL", "wss://exchange.rakuten-wallet.co.jp/ws"),
			APIKey:    getEnv("RAKUTEN_API_KEY", ""),
			APISecret: getEnv("RAKUTEN_API_SECRET", ""),
		},
		LLM: LLMConfig{
			APIKey:      getEnv("ANTHROPIC_API_KEY", ""),
			Model:       getEnv("LLM_MODEL", "claude-haiku-3-5-latest"),
			MaxTokens:   int64(getEnvInt("LLM_MAX_TOKENS", 1024)),
			CacheTTLMin: getEnvInt("LLM_CACHE_TTL_MIN", 15),
		},
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
