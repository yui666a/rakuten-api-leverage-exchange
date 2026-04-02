# Plan 2: Market Data Service + SQLite永続化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** WebSocket/RESTから受信した市場データをchannelでパイプラインに配信し、SQLiteに永続化するMarket Data Serviceを構築する

**Architecture:** Market Data ServiceはユースケースとしてWSClientとRESTClientを利用し、受信したtickデータをchannelで後段（Indicator Calculator等）に配信する。同時にSQLiteリポジトリを通じてローソク足・ティッカーデータを永続化する。WebSocketの自動再接続・2時間タイムアウト対応も含む。

**Tech Stack:** Go 1.21, goroutine/channel, `modernc.org/sqlite`, Plan 1で構築済みのWSClient/RESTClient

---

## ファイル構成

```
backend/
├── go.mod                                              # modernc.org/sqlite 追加
├── internal/
│   ├── domain/
│   │   └── repository/
│   │       └── market_data.go                         # 市場データリポジトリIF
│   ├── usecase/
│   │   └── market_data.go                             # Market Data Service
│   │   └── market_data_test.go
│   └── infrastructure/
│       └── database/
│           ├── sqlite.go                              # SQLite接続管理
│           ├── sqlite_test.go
│           ├── migrations.go                          # スキーマ定義
│           ├── market_data_repo.go                    # リポジトリ実装
│           └── market_data_repo_test.go
```

---

### Task 1: SQLite接続管理

**Files:**
- Modify: `backend/go.mod`
- Create: `backend/internal/infrastructure/database/sqlite.go`
- Create: `backend/internal/infrastructure/database/sqlite_test.go`

- [ ] **Step 1: go.mod に modernc.org/sqlite を追加**

`backend/go.mod` の require ブロックに追加:

```
modernc.org/sqlite v1.34.5
```

```bash
cd backend
go mod tidy
```

- [ ] **Step 2: sqlite_test.go を書く**

```go
package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewDB_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file should exist")
	}
}

func TestNewDB_Ping(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestNewDB_WALMode(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL mode, got %s", journalMode)
	}
}
```

- [ ] **Step 3: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run TestNewDB
```

Expected: コンパイルエラー

- [ ] **Step 4: sqlite.go を実装**

```go
package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// NewDB はSQLiteデータベース接続を作成する。
// WALモードを有効化し、パフォーマンスを最適化する。
func NewDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// WALモード: 読み書きの並行性を向上
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// 外部キー制約を有効化
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
```

- [ ] **Step 5: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run TestNewDB
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add -A
git commit -m "feat: add SQLite database connection with WAL mode"
```

---

### Task 2: マイグレーション（スキーマ定義）

**Files:**
- Create: `backend/internal/infrastructure/database/migrations.go`
- Create: `backend/internal/infrastructure/database/migrations_test.go`

- [ ] **Step 1: migrations_test.go を書く**

```go
package database

import (
	"path/filepath"
	"testing"
)

func TestRunMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// candles テーブルが存在するか確認
	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='candles'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("candles table should exist")
	}

	// tickers テーブルが存在するか確認
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tickers'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("tickers table should exist")
	}

	// trades テーブルが存在するか確認
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='trades'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Fatal("trades table should exist")
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	// 2回実行してもエラーにならない
	if err := RunMigrations(db); err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run TestRunMigrations
```

Expected: コンパイルエラー

- [ ] **Step 3: migrations.go を実装**

```go
package database

import (
	"database/sql"
	"fmt"
)

// RunMigrations はデータベーススキーマを作成する。
// IF NOT EXISTS を使用しているため、冪等に実行できる。
func RunMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS candles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			close REAL NOT NULL,
			volume REAL NOT NULL,
			time INTEGER NOT NULL,
			interval TEXT NOT NULL,
			UNIQUE(symbol_id, time, interval)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_candles_symbol_time
			ON candles(symbol_id, time DESC)`,
		`CREATE TABLE IF NOT EXISTS tickers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			best_ask REAL NOT NULL,
			best_bid REAL NOT NULL,
			open REAL NOT NULL,
			high REAL NOT NULL,
			low REAL NOT NULL,
			last REAL NOT NULL,
			volume REAL NOT NULL,
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tickers_symbol_time
			ON tickers(symbol_id, timestamp DESC)`,
		`CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol_id INTEGER NOT NULL,
			order_side TEXT NOT NULL,
			price REAL NOT NULL,
			amount REAL NOT NULL,
			asset_amount REAL NOT NULL,
			traded_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trades_symbol_time
			ON trades(symbol_id, traded_at DESC)`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run TestRunMigrations
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add database migrations for candles, tickers, and trades"
```

---

### Task 3: 市場データリポジトリインターフェース

**Files:**
- Create: `backend/internal/domain/repository/market_data.go`

- [ ] **Step 1: market_data.go を作成**

```go
package repository

import (
	"context"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// MarketDataRepository は市場データの永続化インターフェースを定義する。
type MarketDataRepository interface {
	// SaveCandle はローソク足データを保存する。重複は無視する。
	SaveCandle(ctx context.Context, symbolID int64, interval string, candle entity.Candle) error

	// SaveCandles は複数のローソク足データを一括保存する。
	SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error

	// GetCandles は指定銘柄・時間足のローソク足を取得する。
	// limit件数を上限に、新しい順で返す。
	GetCandles(ctx context.Context, symbolID int64, interval string, limit int) ([]entity.Candle, error)

	// SaveTicker はティッカーデータを保存する。
	SaveTicker(ctx context.Context, ticker entity.Ticker) error

	// GetLatestTicker は最新のティッカーデータを取得する。
	GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error)
}
```

- [ ] **Step 2: ビルド確認**

```bash
cd backend
go build ./...
```

Expected: ビルド成功

- [ ] **Step 3: コミット**

```bash
git add -A
git commit -m "feat: add MarketDataRepository interface"
```

---

### Task 4: 市場データリポジトリ実装

**Files:**
- Create: `backend/internal/infrastructure/database/market_data_repo.go`
- Create: `backend/internal/infrastructure/database/market_data_repo_test.go`

- [ ] **Step 1: market_data_repo_test.go を書く**

```go
package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

func setupTestDB(t *testing.T) *MarketDataRepo {
	t.Helper()
	tmpDir := t.TempDir()
	db, err := NewDB(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewMarketDataRepo(db)
}

func TestSaveAndGetCandle(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candle := entity.Candle{
		Open: 5000000, High: 5010000, Low: 4990000,
		Close: 5005000, Volume: 10.5, Time: 1700000000000,
	}

	err := repo.SaveCandle(ctx, 7, "PT1M", candle)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	candles, err := repo.GetCandles(ctx, 7, "PT1M", 10)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if len(candles) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(candles))
	}
	if candles[0].Close != 5005000 {
		t.Fatalf("expected close 5005000, got %f", candles[0].Close)
	}
}

func TestSaveCandle_DuplicateIgnored(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candle := entity.Candle{
		Open: 5000000, High: 5010000, Low: 4990000,
		Close: 5005000, Volume: 10.5, Time: 1700000000000,
	}

	_ = repo.SaveCandle(ctx, 7, "PT1M", candle)
	err := repo.SaveCandle(ctx, 7, "PT1M", candle)
	if err != nil {
		t.Fatalf("duplicate save should not error: %v", err)
	}

	candles, err := repo.GetCandles(ctx, 7, "PT1M", 10)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("expected 1 candle (duplicate ignored), got %d", len(candles))
	}
}

func TestSaveCandles_Batch(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candles := []entity.Candle{
		{Open: 5000000, High: 5010000, Low: 4990000, Close: 5005000, Volume: 10.5, Time: 1700000000000},
		{Open: 5005000, High: 5020000, Low: 5000000, Close: 5015000, Volume: 8.3, Time: 1700000060000},
		{Open: 5015000, High: 5025000, Low: 5010000, Close: 5020000, Volume: 12.1, Time: 1700000120000},
	}

	err := repo.SaveCandles(ctx, 7, "PT1M", candles)
	if err != nil {
		t.Fatalf("batch save failed: %v", err)
	}

	result, err := repo.GetCandles(ctx, 7, "PT1M", 10)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 candles, got %d", len(result))
	}

	// 新しい順で返ることを確認
	if result[0].Time != 1700000120000 {
		t.Fatalf("expected newest first, got time %d", result[0].Time)
	}
}

func TestGetCandles_Limit(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	candles := make([]entity.Candle, 5)
	for i := range candles {
		candles[i] = entity.Candle{
			Open: 5000000, High: 5010000, Low: 4990000,
			Close: 5005000, Volume: 10.5,
			Time: int64(1700000000000 + i*60000),
		}
	}
	_ = repo.SaveCandles(ctx, 7, "PT1M", candles)

	result, err := repo.GetCandles(ctx, 7, "PT1M", 3)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 candles (limited), got %d", len(result))
	}
}

func TestGetCandles_FilterBySymbolAndInterval(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_ = repo.SaveCandle(ctx, 7, "PT1M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})
	_ = repo.SaveCandle(ctx, 7, "PT5M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})
	_ = repo.SaveCandle(ctx, 8, "PT1M", entity.Candle{Open: 1, High: 2, Low: 0, Close: 1, Volume: 1, Time: 1000})

	result, err := repo.GetCandles(ctx, 7, "PT1M", 10)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 candle filtered by symbol+interval, got %d", len(result))
	}
}

func TestSaveAndGetTicker(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	ticker := entity.Ticker{
		SymbolID: 7, BestAsk: 5000100, BestBid: 5000000,
		Open: 4900000, High: 5100000, Low: 4800000,
		Last: 5000050, Volume: 123.45, Timestamp: 1700000000000,
	}

	err := repo.SaveTicker(ctx, ticker)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	latest, err := repo.GetLatestTicker(ctx, 7)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if latest.Last != 5000050 {
		t.Fatalf("expected last 5000050, got %f", latest.Last)
	}
}

func TestGetLatestTicker_ReturnsNewest(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})
	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 200, Timestamp: 2000})
	_ = repo.SaveTicker(ctx, entity.Ticker{SymbolID: 7, Last: 150, Timestamp: 1500})

	latest, err := repo.GetLatestTicker(ctx, 7)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if latest.Last != 200 {
		t.Fatalf("expected latest last=200, got %f", latest.Last)
	}
}

func TestGetLatestTicker_NotFound(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.GetLatestTicker(ctx, 999)
	if err == nil {
		t.Fatal("expected error for non-existent symbol")
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run "TestSave|TestGet"
```

Expected: コンパイルエラー

- [ ] **Step 3: market_data_repo.go を実装**

```go
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

type MarketDataRepo struct {
	db *sql.DB
}

func NewMarketDataRepo(db *sql.DB) *MarketDataRepo {
	return &MarketDataRepo{db: db}
}

func (r *MarketDataRepo) SaveCandle(ctx context.Context, symbolID int64, interval string, candle entity.Candle) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO candles (symbol_id, open, high, low, close, volume, time, interval)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		symbolID, candle.Open, candle.High, candle.Low, candle.Close, candle.Volume, candle.Time, interval,
	)
	if err != nil {
		return fmt.Errorf("save candle: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO candles (symbol_id, open, high, low, close, volume, time, interval)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for _, c := range candles {
		if _, err := stmt.ExecContext(ctx, symbolID, c.Open, c.High, c.Low, c.Close, c.Volume, c.Time, interval); err != nil {
			return fmt.Errorf("exec stmt: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) GetCandles(ctx context.Context, symbolID int64, interval string, limit int) ([]entity.Candle, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT open, high, low, close, volume, time FROM candles
		 WHERE symbol_id = ? AND interval = ?
		 ORDER BY time DESC LIMIT ?`,
		symbolID, interval, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query candles: %w", err)
	}
	defer rows.Close()

	var candles []entity.Candle
	for rows.Next() {
		var c entity.Candle
		if err := rows.Scan(&c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Time); err != nil {
			return nil, fmt.Errorf("scan candle: %w", err)
		}
		candles = append(candles, c)
	}
	return candles, rows.Err()
}

func (r *MarketDataRepo) SaveTicker(ctx context.Context, ticker entity.Ticker) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO tickers (symbol_id, best_ask, best_bid, open, high, low, last, volume, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ticker.SymbolID, ticker.BestAsk, ticker.BestBid, ticker.Open, ticker.High,
		ticker.Low, ticker.Last, ticker.Volume, ticker.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save ticker: %w", err)
	}
	return nil
}

func (r *MarketDataRepo) GetLatestTicker(ctx context.Context, symbolID int64) (*entity.Ticker, error) {
	var t entity.Ticker
	err := r.db.QueryRowContext(ctx,
		`SELECT symbol_id, best_ask, best_bid, open, high, low, last, volume, timestamp
		 FROM tickers WHERE symbol_id = ? ORDER BY timestamp DESC LIMIT 1`,
		symbolID,
	).Scan(&t.SymbolID, &t.BestAsk, &t.BestBid, &t.Open, &t.High, &t.Low, &t.Last, &t.Volume, &t.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("get latest ticker: %w", err)
	}
	return &t, nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd backend
go test ./internal/infrastructure/database/ -v -run "TestSave|TestGet"
```

Expected: 全テストPASS

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add MarketDataRepo with SQLite implementation"
```

---

### Task 5: Market Data Service（ユースケース層）

**Files:**
- Create: `backend/internal/usecase/market_data.go`
- Create: `backend/internal/usecase/market_data_test.go`

- [ ] **Step 1: market_data_test.go を書く**

```go
package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// mockMarketDataRepo はテスト用のリポジトリモック
type mockMarketDataRepo struct {
	mu       sync.Mutex
	candles  []entity.Candle
	tickers  []entity.Ticker
}

func newMockRepo() *mockMarketDataRepo {
	return &mockMarketDataRepo{}
}

func (m *mockMarketDataRepo) SaveCandle(_ context.Context, _ int64, _ string, c entity.Candle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candles = append(m.candles, c)
	return nil
}

func (m *mockMarketDataRepo) SaveCandles(_ context.Context, _ int64, _ string, cs []entity.Candle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.candles = append(m.candles, cs...)
	return nil
}

func (m *mockMarketDataRepo) GetCandles(_ context.Context, _ int64, _ string, limit int) ([]entity.Candle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > len(m.candles) {
		limit = len(m.candles)
	}
	return m.candles[:limit], nil
}

func (m *mockMarketDataRepo) SaveTicker(_ context.Context, t entity.Ticker) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickers = append(m.tickers, t)
	return nil
}

func (m *mockMarketDataRepo) GetLatestTicker(_ context.Context, symbolID int64) (*entity.Ticker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.tickers) - 1; i >= 0; i-- {
		if m.tickers[i].SymbolID == symbolID {
			return &m.tickers[i], nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func TestMarketDataService_SubscribeTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tickerCh := svc.SubscribeTicker()

	// ティッカーデータを投入
	svc.HandleTicker(ctx, entity.Ticker{
		SymbolID: 7, Last: 5000000, Timestamp: 1000,
	})

	select {
	case tick := <-tickerCh:
		if tick.Last != 5000000 {
			t.Fatalf("expected last 5000000, got %f", tick.Last)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for ticker")
	}

	// リポジトリにも保存されている
	if len(repo.tickers) != 1 {
		t.Fatalf("expected 1 ticker saved, got %d", len(repo.tickers))
	}
}

func TestMarketDataService_MultipleSubscribers(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch1 := svc.SubscribeTicker()
	ch2 := svc.SubscribeTicker()

	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})

	// 両方のsubscriberがデータを受け取る
	for _, ch := range []<-chan entity.Ticker{ch1, ch2} {
		select {
		case tick := <-ch:
			if tick.Last != 100 {
				t.Fatalf("expected last 100, got %f", tick.Last)
			}
		case <-ctx.Done():
			t.Fatal("timeout")
		}
	}
}

func TestMarketDataService_UnsubscribeTicker(t *testing.T) {
	repo := newMockRepo()
	svc := NewMarketDataService(repo)

	ch := svc.SubscribeTicker()
	svc.UnsubscribeTicker(ch)

	ctx := context.Background()
	svc.HandleTicker(ctx, entity.Ticker{SymbolID: 7, Last: 100, Timestamp: 1000})

	// unsubscribe後はデータを受け取らない
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("should not receive after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		// 期待通り: タイムアウト = データ受信なし
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd backend
go test ./internal/usecase/ -v -run TestMarketDataService
```

Expected: コンパイルエラー

- [ ] **Step 3: market_data.go を実装**

```go
package usecase

import (
	"context"
	"log"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/repository"
)

// MarketDataService は市場データの受信・配信・永続化を管理する。
type MarketDataService struct {
	repo repository.MarketDataRepository

	mu          sync.RWMutex
	tickerSubs  []chan entity.Ticker
}

func NewMarketDataService(repo repository.MarketDataRepository) *MarketDataService {
	return &MarketDataService{
		repo: repo,
	}
}

// SubscribeTicker はティッカーデータの購読を開始し、受信用channelを返す。
func (s *MarketDataService) SubscribeTicker() <-chan entity.Ticker {
	ch := make(chan entity.Ticker, 100)
	s.mu.Lock()
	s.tickerSubs = append(s.tickerSubs, ch)
	s.mu.Unlock()
	return ch
}

// UnsubscribeTicker は購読を解除し、channelを閉じる。
func (s *MarketDataService) UnsubscribeTicker(ch <-chan entity.Ticker) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sub := range s.tickerSubs {
		if sub == ch {
			close(sub)
			s.tickerSubs = append(s.tickerSubs[:i], s.tickerSubs[i+1:]...)
			return
		}
	}
}

// HandleTicker は受信したティッカーデータを永続化し、全subscriberに配信する。
func (s *MarketDataService) HandleTicker(ctx context.Context, ticker entity.Ticker) {
	// 永続化
	if err := s.repo.SaveTicker(ctx, ticker); err != nil {
		log.Printf("failed to save ticker: %v", err)
	}

	// 全subscriberに配信
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.tickerSubs {
		select {
		case ch <- ticker:
		default:
			// channelが満杯の場合はドロップ（遅いconsumerをブロックしない）
		}
	}
}

// GetCandles はリポジトリからローソク足データを取得する。
func (s *MarketDataService) GetCandles(ctx context.Context, symbolID int64, interval string, limit int) ([]entity.Candle, error) {
	return s.repo.GetCandles(ctx, symbolID, interval, limit)
}

// SaveCandles はローソク足データを永続化する。
func (s *MarketDataService) SaveCandles(ctx context.Context, symbolID int64, interval string, candles []entity.Candle) error {
	return s.repo.SaveCandles(ctx, symbolID, interval, candles)
}
```

- [ ] **Step 4: テストにfmtのimportを追加してテストが通ることを確認**

テストファイルの import に `"fmt"` を追加（mockMarketDataRepo.GetLatestTicker で使用）。

```bash
cd backend
go test ./internal/usecase/ -v -run TestMarketDataService
```

Expected: 全テストPASS

- [ ] **Step 5: 全テストを実行して回帰がないことを確認**

```bash
cd backend
go test ./... -v
```

Expected: 全テストPASS

- [ ] **Step 6: コミット**

```bash
git add -A
git commit -m "feat: add MarketDataService with pub/sub ticker distribution"
```

---

### Task 6: config.go にDB設定を追加

**Files:**
- Modify: `backend/config/config.go`

- [ ] **Step 1: config.go にDatabase設定を追加**

`backend/config/config.go` の Config struct に Database フィールドを追加:

```go
type Config struct {
	Server   ServerConfig
	Rakuten  RakutenConfig
	Database DatabaseConfig
}

type DatabaseConfig struct {
	Path string
}
```

Load() 関数の return に追加:

```go
Database: DatabaseConfig{
	Path: getEnv("DATABASE_PATH", "data/trading.db"),
},
```

- [ ] **Step 2: backend/.env.example に追加**

ファイル末尾に追記:

```
# Database
DATABASE_PATH=data/trading.db
```

- [ ] **Step 3: .gitignore に data/ を追加**

プロジェクトルートの `.gitignore` に追加:

```
# Database
data/
*.db
```

- [ ] **Step 4: ビルド確認**

```bash
cd backend
go build ./...
```

Expected: ビルド成功

- [ ] **Step 5: コミット**

```bash
git add -A
git commit -m "feat: add database configuration and .gitignore for SQLite data"
```
