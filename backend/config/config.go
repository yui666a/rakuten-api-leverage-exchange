package config

import (
	"os"
	"strconv"
)

type Config struct {
	Server         ServerConfig
	Rakuten        RakutenConfig
	Database       DatabaseConfig
	Risk           RiskConfig
	LLM            LLMConfig
	Trading        TradingConfig
	Backtest       BacktestConfig
	CircuitBreaker CircuitBreakerConfig
	Reconcile      ReconcileConfig
}

type TradingConfig struct {
	SymbolID             int64   // 取引対象シンボルID（デフォルト: 7 = BTC_JPY）
	TradeAmount          float64 // 1回の注文数量（base currency 単位、例: LTC なら LTC 枚数）。fixed-amount モードではこの値が proposal.Amount に直接渡る。profile の position_sizing.mode が "risk_pct" 等のときは sizer に上書きされる。
	PipelineIntervalSec  int     // パイプライン評価間隔（秒）
	StateSyncIntervalSec int     // ポジション・残高同期間隔（秒）
	MinConfidence        float64 // シグナル最小信頼度（0.0–1.0, デフォルト 0.3）
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

type BacktestConfig struct {
	RetentionDays int // keep backtest results for N days (default: 180)
}

// CircuitBreaker is exposed as a top-level Config field by Load() below.

type RiskConfig struct {
	MaxPositionAmount     float64
	MaxDailyLoss          float64
	StopLossPercent       float64
	StopLossATRMultiplier float64
	TakeProfitPercent     float64
	InitialCapital        float64
	MaxConsecutiveLosses  int
	CooldownMinutes       int
	// Pre-trade orderbook depth gate. Both default to 0 (disabled) so the
	// gate stays opt-in until the user has confidence in the live cache.
	MaxSlippageBps float64
	MaxBookSidePct float64
}

// CircuitBreakerConfig mirrors usecase/circuitbreaker/Config but lives here
// so the composition root can populate it from env vars without importing the
// usecase layer into config/.
type CircuitBreakerConfig struct {
	AbnormalSpreadPct    float64
	AbnormalSpreadHoldMs int64
	PriceJumpPct         float64
	PriceJumpWindowMs    int64
	BookFeedStaleAfterMs int64
	EmptyBookHoldMs      int64
	StaleCheckIntervalMs int64
}

// ReconcileConfig mirrors usecase/reconcile/Config. opt-in.
type ReconcileConfig struct {
	Enable          bool
	IntervalSec     int
	PositionWarnPct float64
	PositionHaltPct float64
	BalanceWarnPct  float64
	BalanceHaltPct  float64
	OrderTTLSec     int
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
			MaxPositionAmount:     getEnvFloat("RISK_MAX_POSITION_AMOUNT", 5000),
			MaxDailyLoss:          getEnvFloat("RISK_MAX_DAILY_LOSS", 5000),
			StopLossPercent:       getEnvFloat("RISK_STOP_LOSS_PERCENT", 5),
			StopLossATRMultiplier: getEnvFloat("RISK_STOP_LOSS_ATR_MULTIPLIER", 2.0),
			TakeProfitPercent:     getEnvFloat("RISK_TAKE_PROFIT_PERCENT", 10),
			InitialCapital:        getEnvFloat("RISK_INITIAL_CAPITAL", 10000),
			MaxConsecutiveLosses:  getEnvInt("RISK_MAX_CONSECUTIVE_LOSSES", 3),
			CooldownMinutes:       getEnvInt("RISK_COOLDOWN_MINUTES", 30),
			MaxSlippageBps:        getEnvFloat("RISK_MAX_SLIPPAGE_BPS", 0),
			MaxBookSidePct:        getEnvFloat("RISK_MAX_BOOK_SIDE_PCT", 0),
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
			MinConfidence:        getEnvFloat("TRADE_MIN_CONFIDENCE", 0.3),
		},
		Backtest: BacktestConfig{
			RetentionDays: getEnvInt("BACKTEST_RETENTION_DAYS", 180),
		},
		Reconcile: ReconcileConfig{
			Enable:          getEnvBool("RECONCILE_ENABLED", false),
			IntervalSec:     getEnvInt("RECONCILE_INTERVAL_SEC", 60),
			PositionWarnPct: getEnvFloat("RECONCILE_POSITION_WARN_PCT", 0.05),
			PositionHaltPct: getEnvFloat("RECONCILE_POSITION_HALT_PCT", 0.5),
			BalanceWarnPct:  getEnvFloat("RECONCILE_BALANCE_WARN_PCT", 0.01),
			BalanceHaltPct:  getEnvFloat("RECONCILE_BALANCE_HALT_PCT", 0.05),
			OrderTTLSec:     getEnvInt("RECONCILE_ORDER_TTL_SEC", 300),
		},
		CircuitBreaker: CircuitBreakerConfig{
			// Default = OFF. Operators flip the env vars on once they're
			// happy with the live book cache so a misconfigured threshold
			// never silently halts a fresh deploy.
			AbnormalSpreadPct:    getEnvFloat("CB_ABNORMAL_SPREAD_PCT", 0),
			AbnormalSpreadHoldMs: int64(getEnvInt("CB_ABNORMAL_SPREAD_HOLD_MS", 5_000)),
			PriceJumpPct:         getEnvFloat("CB_PRICE_JUMP_PCT", 0),
			PriceJumpWindowMs:    int64(getEnvInt("CB_PRICE_JUMP_WINDOW_MS", 60_000)),
			BookFeedStaleAfterMs: int64(getEnvInt("CB_BOOK_FEED_STALE_AFTER_MS", 0)),
			EmptyBookHoldMs:      int64(getEnvInt("CB_EMPTY_BOOK_HOLD_MS", 0)),
			StaleCheckIntervalMs: int64(getEnvInt("CB_STALE_CHECK_INTERVAL_MS", 5_000)),
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		switch value {
		case "1", "true", "True", "TRUE", "yes", "on":
			return true
		case "0", "false", "False", "FALSE", "no", "off":
			return false
		}
	}
	return defaultValue
}
