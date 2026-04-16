# PDCA Strategy Optimizer — 設計書

**日付:** 2026-04-16
**対象通貨:** LTC/JPY 固定
**目的:** バックテストを自動化し、PDCAサイクルでパラメータ調整→ロジック組み替え→新指標追加を段階的に行い、最適な戦略を発見する

---

## 1. 概要

### 1.1 目標

- MaxDrawdown 20%以下の制約のもとで Total Return を最大化する
- 2週間区間での勝率80%を目標とする（バックテスト期間を2週間ウィンドウでスライドし、各ウィンドウの勝率を算出、その平均値で評価）
- 本番運用の戦略とは独立して実験的な戦略を試行できるようにする
- うまくいった戦略を手動承認で本番に昇格させる

### 1.2 方針まとめ

| 項目 | 決定 |
|---|---|
| 通貨 | LTC/JPY 固定 |
| チューニング範囲 | パラメータ → ロジック組み替え → 新指標追加（全レベル） |
| AI自律度 | 半自律（コード変更+バックテスト自動、本番反映は手動承認） |
| 実行トリガー | 手動起動（Claude Codeセッション内） |
| 評価基準 | MaxDD上限付きリターン最大化 + 2週間勝率80%目標 |
| AI実装方式 | Claude Codeセッション内で完結 |

---

## 2. 戦略プラグインアーキテクチャ

### 2.1 Strategy Interface

現在の `StrategyEngine` と `RuleBasedStanceResolver` をinterfaceの背後に配置し、複数の戦略実装を切り替え可能にする。

```go
// domain/port/strategy.go
type Strategy interface {
    Evaluate(ctx context.Context, indicators *entity.IndicatorSet, higherTF *entity.IndicatorSet, lastPrice float64, now time.Time) (*entity.Signal, error)
    Name() string
}
```

### 2.2 実装

- **DefaultStrategy** — 現行の StrategyEngine + RuleBasedStanceResolver をラップ。本番デフォルト。
- **ConfigurableStrategy** — StrategyProfile (JSON) からパラメータと条件を構築。バックテスト実験用。

### 2.3 Registry

```go
// usecase/strategy/registry.go
type StrategyRegistry struct {
    strategies map[string]Strategy
}

func (r *StrategyRegistry) Register(name string, s Strategy)
func (r *StrategyRegistry) Get(name string) (Strategy, error)
func (r *StrategyRegistry) List() []string
```

---

## 3. StrategyProfile 構造

バックテストに渡す設定ファイル。戦略の全パラメータを宣言的に定義する。

### 3.1 ファイル配置

```
backend/profiles/
├── production.json              ← 本番用
└── experiment_*.json            ← PDCA実験用
```

### 3.2 JSON構造

```json
{
  "name": "ltc_aggressive_v3",
  "description": "LTC向け攻めの短期戦略",
  "indicators": {
    "sma_short": 10,
    "sma_long": 30,
    "rsi_period": 14,
    "macd_fast": 12,
    "macd_slow": 26,
    "macd_signal": 9,
    "bb_period": 20,
    "bb_multiplier": 2.0,
    "atr_period": 14
  },
  "stance_rules": {
    "rsi_oversold": 20,
    "rsi_overbought": 80,
    "sma_convergence_threshold": 0.001,
    "bb_squeeze_lookback": 5,
    "breakout_volume_ratio": 1.5
  },
  "signal_rules": {
    "trend_follow": {
      "enabled": true,
      "require_macd_confirm": true,
      "require_ema_cross": true,
      "rsi_buy_max": 70,
      "rsi_sell_min": 30
    },
    "contrarian": {
      "enabled": true,
      "rsi_entry": 30,
      "rsi_exit": 70,
      "macd_histogram_limit": 10
    },
    "breakout": {
      "enabled": true,
      "volume_ratio_min": 1.5,
      "require_macd_confirm": true
    }
  },
  "strategy_risk": {
    "stop_loss_percent": 5,
    "take_profit_percent": 10,
    "stop_loss_atr_multiplier": 0,
    "max_position_amount": 100000,
    "max_daily_loss": 50000
  },
  "htf_filter": {
    "enabled": true,
    "block_counter_trend": true,
    "alignment_boost": 0.1
  }
}
```

### 3.3 Go構造体

```go
// domain/entity/strategy_config.go
type StrategyProfile struct {
    Name        string              `json:"name"`
    Description string              `json:"description"`
    Indicators  IndicatorConfig     `json:"indicators"`
    StanceRules StanceRulesConfig   `json:"stance_rules"`
    SignalRules SignalRulesConfig    `json:"signal_rules"`
    Risk        StrategyRiskConfig  `json:"strategy_risk"`
    HTFFilter   HTFFilterConfig     `json:"htf_filter"`
}

type IndicatorConfig struct {
    SMAShort     int     `json:"sma_short"`
    SMALong      int     `json:"sma_long"`
    RSIPeriod    int     `json:"rsi_period"`
    MACDFast     int     `json:"macd_fast"`
    MACDSlow     int     `json:"macd_slow"`
    MACDSignal   int     `json:"macd_signal"`
    BBPeriod     int     `json:"bb_period"`
    BBMultiplier float64 `json:"bb_multiplier"`
    ATRPeriod    int     `json:"atr_period"`
}

type StanceRulesConfig struct {
    RSIOversold              float64 `json:"rsi_oversold"`
    RSIOverbought            float64 `json:"rsi_overbought"`
    SMAConvergenceThreshold  float64 `json:"sma_convergence_threshold"`
    BBSqueezeLookback        int     `json:"bb_squeeze_lookback"`
    BreakoutVolumeRatio      float64 `json:"breakout_volume_ratio"`
}

type TrendFollowConfig struct {
    Enabled            bool    `json:"enabled"`
    RequireMACDConfirm bool    `json:"require_macd_confirm"`
    RequireEMACross    bool    `json:"require_ema_cross"`
    RSIBuyMax          float64 `json:"rsi_buy_max"`
    RSISellMin         float64 `json:"rsi_sell_min"`
}

type ContrarianConfig struct {
    Enabled            bool    `json:"enabled"`
    RSIEntry           float64 `json:"rsi_entry"`
    RSIExit            float64 `json:"rsi_exit"`
    MACDHistogramLimit float64 `json:"macd_histogram_limit"`
}

type BreakoutConfig struct {
    Enabled            bool    `json:"enabled"`
    VolumeRatioMin     float64 `json:"volume_ratio_min"`
    RequireMACDConfirm bool    `json:"require_macd_confirm"`
}

type SignalRulesConfig struct {
    TrendFollow TrendFollowConfig `json:"trend_follow"`
    Contrarian  ContrarianConfig  `json:"contrarian"`
    Breakout    BreakoutConfig    `json:"breakout"`
}

type HTFFilterConfig struct {
    Enabled           bool    `json:"enabled"`
    BlockCounterTrend bool    `json:"block_counter_trend"`
    AlignmentBoost    float64 `json:"alignment_boost"`
}

type StrategyRiskConfig struct {
    StopLossPercent      float64 `json:"stop_loss_percent"`
    TakeProfitPercent    float64 `json:"take_profit_percent"`
    StopLossATRMultiplier float64 `json:"stop_loss_atr_multiplier"`
    MaxPositionAmount    float64 `json:"max_position_amount"`
    MaxDailyLoss         float64 `json:"max_daily_loss"`
}
```

---

## 4. PDCAサイクル

### 4.1 実行フロー

```
あなた: 「LTCの戦略を最適化して」

Plan:
  1. production.json を読む
  2. docs/pdca/ の過去履歴を読む
  3. 直近バックテスト結果を分析
  4. 改善仮説を立てる

Do:
  1. profiles/experiment_*.json を生成
  2. (Level 3の場合) indicator/*.go を追加・修正
  3. go run ./cmd/backtest run --profile profiles/experiment_... を実行

Check:
  1. 結果を前回と比較 (目的関数で評価)
     - [必須制約] MaxDD ≤ 20% — 超過した場合は即 reject
     - [主目的] Total Return 最大化
     - [副目的] 2週間スライド勝率 → 80%目標
     - [参考] Sharpe Ratio, ProfitFactor

Act:
  1. 改善 → 採用、次の仮説へ
  2. 悪化 → ロールバック
  3. docs/pdca/YYYY-MM-DD_cycleNN.md に記録
  4. あなたに報告 →「次のサイクル回しますか？」
```

### 4.2 段階的エスカレーション

| サイクル | レベル | 内容 | 例 |
|---|---|---|---|
| 1〜3 | Level 1: パラメータ | 数値の調整 | RSI閾値、SMA期間、SL/TP% |
| 4〜6 | Level 2: 条件組替 | ロジック構造の変更 | MACD確認を外す、BB Squeeze厳格化 |
| 7〜 | Level 3: 新指標 | Goコード追加 | ADX、Stochastics、Ichimoku等 |

頭打ちになったら次のレベルに上がる。

### 4.3 PDCA記録フォーマット

```markdown
# PDCA Cycle NN — YYYY-MM-DD

## 仮説
(何をどう変えるか、なぜ改善すると考えるか)

## 変更内容
- プロファイル: profiles/experiment_YYYY-MM-DD_NN.json
- 変更パラメータ一覧

## 結果
| 指標 | before | after | 判定 |
|---|---|---|---|
| Total Return | | | |
| MaxDD | | | |
| 2週間勝率 | | | |
| Sharpe Ratio | | | |
| WinRate | | | |
| ProfitFactor | | | |

## 判定
(採用 / ロールバック / 部分改善)

## 学び
(次のサイクルに活かす知見)
```

### 4.4 ファイル配置

```
docs/pdca/
├── 2026-04-16_cycle01.md
├── 2026-04-16_cycle02.md
└── ...
```

---

## 5. バックテスト結果の統合

### 5.1 DBスキーマ追加

既存の `backtest_results` テーブルに以下のカラムを追加する。
現行の `addColumnIfNotExists` 補助関数（`database/migrations.go`）を使い、冪等に実行する。

```go
// migrations.go の RunMigrations 末尾に追加
backtestPDCAColumns := []struct {
    name string
    def  string
}{
    {"profile_name", "profile_name TEXT NOT NULL DEFAULT ''"},
    {"pdca_cycle_id", "pdca_cycle_id TEXT NOT NULL DEFAULT ''"},
    {"hypothesis", "hypothesis TEXT NOT NULL DEFAULT ''"},
    {"parent_result_id", "parent_result_id TEXT NOT NULL DEFAULT ''"},
    {"biweekly_win_rate", "biweekly_win_rate REAL NOT NULL DEFAULT 0"},
}
for _, col := range backtestPDCAColumns {
    if err := addColumnIfNotExists(db, "backtest_results", col.name, col.def); err != nil {
        return fmt.Errorf("backtest_results alter: %w", err)
    }
}
```

`parent_result_id` にはインデックスを作成し、系譜追跡のクエリ性能を確保する。
SQLite では FK 制約の実効性が限定的なため、アプリケーション層で参照整合性を検証する。

```sql
CREATE INDEX IF NOT EXISTS idx_backtest_results_parent
    ON backtest_results(parent_result_id)
    WHERE parent_result_id != '';

CREATE INDEX IF NOT EXISTS idx_backtest_results_profile
    ON backtest_results(profile_name)
    WHERE profile_name != '';

CREATE INDEX IF NOT EXISTS idx_backtest_results_pdca_cycle
    ON backtest_results(pdca_cycle_id)
    WHERE pdca_cycle_id != '';
```

`biweekly_win_rate` カラムは `BacktestSummary` の新フィールドに対応し、Repository の INSERT/SELECT で永続化する。

### 5.2 API レスポンス拡張

`BacktestResult` に以下のフィールドを追加:

```go
type BacktestResult struct {
    // ... 既存フィールド
    ProfileName    string `json:"profileName"`
    PDCACycleID    string `json:"pdcaCycleId,omitempty"`
    Hypothesis     string `json:"hypothesis,omitempty"`
    ParentResultID string `json:"parentResultId,omitempty"`
}
```

### 5.3 一覧 API のフィルタ拡張

既存の `BacktestResultFilter` (現在は `Limit`/`Offset` のみ) にフィルタパラメータを追加する。

```go
// domain/repository/backtest_result.go
type BacktestResultFilter struct {
    Limit          int
    Offset         int
    ProfileName    string // 完全一致フィルタ (空文字 = フィルタなし)
    PDCACycleID    string // 完全一致フィルタ
    ParentResultID string // 系譜追跡用
}
```

```
GET /api/v1/backtest/results?limit=20&offset=0&profileName=ltc_aggressive_v3&pdcaCycleId=2026-04-16_cycle01
```

Repository 実装では追加カラムを `WHERE` 句に動的追加する。

### 5.4 フロントエンド一覧表示

バックテスト一覧にプロファイル名・PDCAサイクル番号を表示:

- プロファイル名でフィルタ可能 (ドロップダウン)
- PDCAサイクルの結果は改善チェーン (parent_result_id) で系譜を辿れる
- 実験結果と手動実行を視覚的に区別 (アイコン or バッジ)

---

## 6. 新指標追加の拡張ポイント

PDCA Level 3 で新しいインジケーターを追加する際の変更箇所:

```
1. infrastructure/indicator/<name>.go    — 計算ロジック
2. domain/entity/indicator.go            — IndicatorSet にフィールド追加
3. usecase/strategy/configurable_strategy.go — シグナル条件で参照
```

### 6.1 追加候補指標

| 指標 | 用途 | 現状 | 追加難易度 |
|---|---|---|---|
| Stochastics | オーバーシュート検知 | フロントのみ | 低 |
| Ichimoku | 雲・転換線でトレンド判定 | フロントのみ | 中 |
| ADX | トレンド強度フィルター | なし | 低 |
| Williams %R | オーバーシュート検知 | なし | 低 |
| OBV | 出来高トレンド確認 | なし | 低 |
| VWAP | 日中の価格重心 | なし | 中 |

---

## 7. 評価関数 (目的関数)

### 7.1 PDCA用複合スコア

現行の Optimizer は Sharpe Ratio 単一でソートしているが、PDCA では以下の複合評価を適用する。

```go
// PDCAの評価ロジック (Claude Code セッション内で判定)
// 実装としては reporter.go の BacktestSummary に 2週間勝率フィールドを追加し、
// CLI出力に含める。最終判定は Claude Code が行う。

type PDCAEvaluation struct {
    // 必須制約 — 1つでも違反したら reject
    MaxDDConstraint     float64 // ≤ 0.20 (20%)

    // 主目的 — 高いほど良い
    TotalReturn         float64

    // 副目的 — 高いほど良い (目標 0.80)
    BiweeklyWinRate     float64 // 2週間スライドウィンドウ勝率の平均

    // 参考指標
    SharpeRatio         float64
    ProfitFactor        float64
    WinRate             float64
}
```

### 7.2 2週間スライド勝率の計算

バックテスト期間をタイムスタンプベースで2週間 (14日) のウィンドウに分割し、1日ずつスライドする。
各ウィンドウ内のクローズ済みトレードから勝率を算出し、全ウィンドウの平均を取る。

**最低トレード数制約:** 各ウィンドウ内のトレード数が3件未満の場合はそのウィンドウの勝率を0%として扱う（スキップではなくペナルティ）。
これにより低頻度売買戦略が見かけ上高い勝率を得ることを防ぐ。

**カバレッジ率制約:** トレードが3件以上あるウィンドウの割合（カバレッジ率）が50%未満の場合、BiweeklyWinRate 自体を信頼不可として 0 を返す。

```
期間: 2024-01-01 ～ 2024-12-31
Window 1: 01/01 - 01/14 → 5件, 勝率 75%  ✓
Window 2: 01/02 - 01/15 → 1件, 勝率 0%   (3件未満ペナルティ)
Window 3: 01/03 - 01/16 → 4件, 勝率 80%  ✓
...
カバレッジ率 = 有効ウィンドウ数 / 全ウィンドウ数
BiweeklyWinRate = カバレッジ率 ≥ 50% ? avg(全ウィンドウ) : 0
```

### 7.3 BacktestSummary への追加フィールド

```go
type BacktestSummary struct {
    // ... 既存フィールド
    BiweeklyWinRate float64 // 2週間スライド勝率の平均
}
```

---

## 8. CLI 拡張

### 8.1 `--profile` フラグ追加

CLI および API でのパスは `backend/` ディレクトリからの相対パスとして解決する。
これは既存の `--data` フラグ（例: `data/candles_*.csv`）と同じ規約に従う。

```bash
# 実行ディレクトリ: backend/
cd backend

# プロファイル指定でバックテスト実行
go run ./cmd/backtest run \
  --profile profiles/experiment_2026-04-16_01.json \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv

# プロファイル指定で最適化
go run ./cmd/backtest optimize \
  --profile profiles/base.json \
  --param "stop_loss_percent=1:10:1" \
  ...
```

### 8.2 API 拡張

```
POST /api/v1/backtest/run
  + profileName: string (プロファイル名。profiles/<profileName>.json として解決)
  + pdcaCycleId: string (PDCAサイクルID、オプション)
  + hypothesis: string (仮説テキスト、オプション)
  + parentResultId: string (比較元の結果ID、オプション)
```

### 8.3 プロファイルパスのバリデーション

API 経由の `profileName` はファイル名のみ（拡張子なし）を受け付け、サーバー側で `profiles/<name>.json` に解決する。
CLI の `--profile` も同様に `profiles/` 配下に限定する。

```go
// パストラバーサル防止
func resolveProfilePath(name string) (string, error) {
    // 英数字、ハイフン、アンダースコアのみ許可
    if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(name) {
        return "", fmt.Errorf("invalid profile name: %s", name)
    }
    path := filepath.Join("profiles", name+".json")
    // 念のため Clean 後に profiles/ プレフィックスを検証
    cleaned := filepath.Clean(path)
    if !strings.HasPrefix(cleaned, "profiles/") {
        return "", fmt.Errorf("profile path traversal detected: %s", name)
    }
    return cleaned, nil
}
```

---

## 9. スコープ

### 今回実装するもの

- Strategy interface + StrategyRegistry
- StrategyProfile (JSON) 構造体 + パース
- ConfigurableStrategy (プロファイル駆動の戦略)
- DefaultStrategy (現行ロジックのラップ)
- backtest_results への PDCAメタデータカラム追加
- CLI `--profile` フラグ対応
- API リクエスト/レスポンスの拡張
- バックテスト一覧画面にプロファイル名・PDCAサイクル情報の表示
- production.json (現行パラメータの外出し)
- docs/pdca/ 記録フォーマット
- BiweeklyWinRate (2週間スライド勝率) の算出ロジック + BacktestSummary への追加

### 今回実装しないもの

- フロントの「本番昇格」ボタン (手動でJSONコピー)
- 新指標の実装 (PDCA Level 3 で都度追加)
- 自動実行トリガー (cron等)
- 複数通貨対応 (LTC/JPY固定)
