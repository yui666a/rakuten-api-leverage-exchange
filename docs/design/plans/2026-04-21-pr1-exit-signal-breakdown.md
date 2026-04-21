# PR-1: Exit 理由別 / シグナル別サマリ

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: A（観測側強化）
**Stacked PR 順序**: #1 / 6（main の直上）
**見積もり**: 1 日
**ステータス**: ✅ **merged** — #108 にて main に取り込み済み (2026-04-21)。本 Doc は as-built として残す。

## 動機

cycle05 / cycle06 で各シグナル（trend_follow / contrarian / breakout）の寄与を知るために、わざわざ `enabled=false` にして差分推定した。非効率で比較も不正確。
同様に「TP で利確できたトレード」「SL で損切られたトレード」「reverse_signal で閉じたトレード」の内訳が見えないため、TP / SL パラメータ調整の判断材料が不足していた（cycle04 の TP 15% が失敗した理由を推測するのに時間がかかった）。

**1 回のバックテスト結果でこれらの内訳がレスポンスに含まれていれば解決**する。

## 対象外

- トレードごとの詳細情報（既に `trades` フィールドで返却済み）
- 期間分割集計（PR-2 で別途）
- regime 別集計（PR-5 で別途）

## 仕様

### データモデル

```go
// entity/backtest.go
type SummaryBreakdown struct {
    Trades       int     `json:"trades"`
    WinTrades    int     `json:"winTrades"`
    LossTrades   int     `json:"lossTrades"`
    WinRate      float64 `json:"winRate"`       // 0-100
    TotalPnL     float64 `json:"totalPnL"`      // JPY
    AvgPnL       float64 `json:"avgPnL"`        // JPY/trade
    ProfitFactor float64 `json:"profitFactor"`  // SumWins / |SumLosses|; 0 if no losses and no wins
}

type BacktestSummary struct {
    // ... 既存フィールドはそのまま ...

    ByExitReason   map[string]SummaryBreakdown `json:"byExitReason"`   // "reverse_signal" / "stop_loss" / "take_profit" / "end_of_test"
    BySignalSource map[string]SummaryBreakdown `json:"bySignalSource"` // "trend_follow" / "contrarian" / "breakout"
}
```

### Signal Source の抽出

`BacktestTradeRecord.ReasonEntry` は既に人間可読な文字列（`"trend follow: EMA12 > EMA26, SMA aligned, RSI not overbought, MACD confirmed"` のような形式）。

実装は **コロンで終端するプレフィックス**で判定する（`"trend follow:"` 等）。コロンを含めることで、仮に将来 `"trend follower xyz"` のような別フレーズが出てもハイジャックされないよう守っている。

```go
// backend/internal/usecase/backtest/breakdown.go（実装済み）
func parseSignalSource(reasonEntry string) string {
    lower := strings.ToLower(strings.TrimSpace(reasonEntry))
    switch {
    case strings.HasPrefix(lower, "trend follow:"):
        return SignalSourceTrendFollow
    case strings.HasPrefix(lower, "contrarian:"):
        return SignalSourceContrarian
    case strings.HasPrefix(lower, "breakout:"):
        return SignalSourceBreakout
    default:
        return SignalSourceUnknown
    }
}
```

**設計判断**: enum を新設するより文字列プレフィックス判定で十分（発火箇所が `strategy.go` の数か所のみで、文言は安定）。リスクは strategy.go の reason 文言が変わった時に breakdown が `unknown` に流れること。これを防ぐため `parseSignalSource` と strategy.go 双方の定数化は次の大きなリファクタで対応する（PR-1 のスコープ外）。

### 集計関数

```go
func BuildBreakdown(trades []BacktestTradeRecord, keyFunc func(BacktestTradeRecord) string) map[string]SummaryBreakdown {
    buckets := map[string][]BacktestTradeRecord{}
    for _, t := range trades {
        key := keyFunc(t)
        buckets[key] = append(buckets[key], t)
    }
    out := make(map[string]SummaryBreakdown, len(buckets))
    for key, ts := range buckets {
        out[key] = computeBreakdown(ts)
    }
    return out
}

func computeBreakdown(trades []BacktestTradeRecord) SummaryBreakdown {
    // wins, losses, sumWins, sumLosses を集計して SummaryBreakdown に詰める
}
```

`SummaryReporter.BuildSummary` で末尾に 2 回呼ぶ:
```go
summary.ByExitReason = BuildBreakdown(trades, func(t BacktestTradeRecord) string { return t.ReasonExit })
summary.BySignalSource = BuildBreakdown(trades, func(t BacktestTradeRecord) string { return parseSignalSource(t.ReasonEntry) })
```

### DB マイグレーション

**選択肢A（採用）**: `backtest_results` に `breakdown_json TEXT NULL` カラムを 1 本追加し、上記 2 map を JSON シリアライズして格納。
**選択肢B（不採用）**: 正規化テーブル `backtest_result_breakdowns`。理由: クエリで JOIN する要件がなく、read 専用で常に全フィールドと一緒に取る用途しかないため過剰設計。

マイグレーション SQL:
```sql
ALTER TABLE backtest_results ADD COLUMN breakdown_json TEXT DEFAULT NULL;
```

レガシー行（この PR マージ前に作成された行）は `breakdown_json = NULL`。読み出し時に NULL なら `ByExitReason / BySignalSource` は空 map として返す（後方互換）。

### API 変更

`GET /backtest/results` / `POST /backtest/run` レスポンスの `summary` に上記 2 フィールドが追加されるのみ。既存 consumer は無視で動く（マップが空でも正常）。

### Frontend

変更最小ケース: バックテスト詳細ページに **「シグナル別」「Exit 理由別」のテーブル 2 個**を追加。列は `Trades / WinRate / PF / TotalPnL / AvgPnL`。
既存のサマリ数値カードは触らない。

## テスト計画

### Unit

1. `parseSignalSource` — 既存 strategy.go の全 reasonEntry 文言パターンに対して期待 source が返る（`"trend follow: ..."` / `"contrarian: ..."` / `"breakout: ..."` / 空文字 → `"unknown"`）
2. `computeBreakdown` — 手動で作った trades（5 win / 3 loss）に対して期待値が一致
3. `BuildBreakdown` — 複数キーで正しくバケット化される
4. JSON 往復 — `Marshal → Unmarshal` で `ByExitReason / BySignalSource` の値が保たれる

### Integration

1. `runner_test.go` に 1 ケース追加: 既存テストシナリオで `summary.ByExitReason` / `summary.BySignalSource` に期待 key が含まれ、全 key の `Trades` 合計が `summary.TotalTrades` に一致
2. `result_repository_test.go`: Save → Load で breakdown が往復する（JSON 永続化の確認）
3. `migrations_test.go`: マイグレーション再実行で壊れない（既存 idempotent テストの枠内）

## DoD（as-built）

PR-1 (BE) のスコープ内で達成した項目のみ [x]。FE 表示は当初スコープ外に切り出した（下記「フォローアップ」参照）。

- [x] unit test 15 本 passing（当初見積 4 本から拡大: parseSignalSource 10 ケース + computeBreakdown 5 ケース）
- [x] integration test 3 本 passing（reporter / repo round-trip / legacy 互換）
- [x] 既存 `TestConfigurableStrategy_EquivalentToDefault` が通る（挙動不変）
- [x] 新規マイグレーション が `migrations_test.go` で idempotent、`breakdown_json` 列存在を明示 assert
- [x] レガシー行（breakdown_json NULL）を読めることを確認
- [x] PR 本文: 直近 6 ヶ月 production バックテスト結果の各 map を貼付（#108 本文参照）

### フォローアップ（別 PR）

- Frontend にバケット別テーブル 2 個を追加し、`pnpm test` を pass させる。BE の JSON には既に含まれているため、FE 側は表示コンポーネントのみ

## ロールバック

マイグレーション追加の逆SQL（カラム削除）は SQLite で不可。ただしカラムは NULL 許容で副作用なし。PR 全体を revert するだけで影響最小。

## 備考

- この PR は **後続 PR-2 〜 PR-13 全ての効果測定の器**。必ず最初にマージする
- `parseSignalSource` が `strategy.go` の reason 文言に暗黙依存する点は `strategy_test.go` に「reason 文言と breakdown キーの対応」を assert することで保護する
