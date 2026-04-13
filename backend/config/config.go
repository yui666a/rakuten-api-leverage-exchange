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
	Trading  TradingConfig
}

type TradingConfig struct {
	SymbolID            int64   // 取引対象シンボルID（デフォルト: 7 = BTC_JPY）
	TradeAmount         float64 // 1回の注文金額（円）
	PipelineIntervalSec int     // パイプライン評価間隔（秒）
	StateSyncIntervalSec int    // ポジション・残高同期間隔（秒）
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
	MaxPositionAmount    float64
	MaxDailyLoss         float64
	StopLossPercent      float64
	TakeProfitPercent    float64
	InitialCapital       float64
	MaxConsecutiveLosses int
	CooldownMinutes      int
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
			MaxPositionAmount:    getEnvFloat("RISK_MAX_POSITION_AMOUNT", 5000),
			MaxDailyLoss:         getEnvFloat("RISK_MAX_DAILY_LOSS", 5000),
			StopLossPercent:      getEnvFloat("RISK_STOP_LOSS_PERCENT", 5),
			TakeProfitPercent:    getEnvFloat("RISK_TAKE_PROFIT_PERCENT", 10),
			InitialCapital:       getEnvFloat("RISK_INITIAL_CAPITAL", 10000),
			MaxConsecutiveLosses: getEnvInt("RISK_MAX_CONSECUTIVE_LOSSES", 3),
			CooldownMinutes:      getEnvInt("RISK_COOLDOWN_MINUTES", 30),
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
		Trading: TradingConfig{
			SymbolID:             int64(getEnvInt("TRADE_SYMBOL_ID", 7)),
			TradeAmount:          getEnvFloat("TRADE_AMOUNT", 1000),
			PipelineIntervalSec:  getEnvInt("PIPELINE_INTERVAL_SEC", 60),
			StateSyncIntervalSec: getEnvInt("STATE_SYNC_INTERVAL_SEC", 15),
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
