# PR-2: 複数期間一括バックテスト API + 頑健性スコア

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: A（観測側強化）
**Stacked PR 順序**: #2 / 6（PR-1 の上）
**見積もり**: 2 日

## 動機

v3 昇格時、候補ごとに 1yr/2yr/3yr/四半期 を手動で curl 7-8 回叩いて Python で集計した。過学習判定に必須の作業にもかかわらず摩擦が大きい。**1 プロファイル × N 期間を 1 リクエストで実行し、頑健性スコアを返すエンドポイント**があれば PDCA 回転速度が再度ケタ違いに上がる。

## 対象外

- walk-forward（PR-13 で別途）
- 複数プロファイル同時実行（N 件を直列に叩けばいいので不要）
- regime 別集計（PR-5 で別途）

## 仕様

### データモデル

```go
// entity/backtest.go
type MultiPeriodRequest struct {
    ProfileName    string         `json:"profileName"`
    Data           string         `json:"data"`
    DataHtf        string         `json:"dataHtf"`
    InitialBalance float64        `json:"initialBalance"`
    TradeAmount    float64        `json:"tradeAmount"`
    SpreadPercent  float64        `json:"spreadPercent"`
    Slippage       float64        `json:"slippagePercent"`
    Periods        []PeriodSpec   `json:"periods"`    // 必須、1 件以上
    PdcaCycleId    string         `json:"pdcaCycleId,omitempty"`
    Hypothesis     string         `json:"hypothesis,omitempty"`
    ParentResultId string         `json:"parentResultId,omitempty"`
}

type PeriodSpec struct {
    Label string `json:"label"`  // 人間可読、例 "1yr"
    From  string `json:"from"`   // "YYYY-MM-DD"
    To    string `json:"to"`
}

type MultiPeriodResult struct {
    ID         string                   `json:"id"`          // まとめの ULID
    CreatedAt  int64                    `json:"createdAt"`
    ProfileName string                  `json:"profileName"`
    Periods    []LabeledBacktestResult  `json:"periods"`     // 個別結果
    Aggregate  MultiPeriodAggregate     `json:"aggregate"`
}

type LabeledBacktestResult struct {
    Label  string          `json:"label"`
    Result BacktestResult  `json:"result"`  // 既存型。breakdown 込み (PR-1)
}

type MultiPeriodAggregate struct {
    GeomMeanReturn   float64 `json:"geomMeanReturn"`    // 各期間 Return の幾何平均
    ReturnStdDev     float64 `json:"returnStdDev"`
    WorstReturn      float64 `json:"worstReturn"`
    BestReturn       float64 `json:"bestReturn"`
    WorstDrawdown    float64 `json:"worstDrawdown"`
    AllPositive      bool    `json:"allPositive"`       // 全期間プラス
    RobustnessScore  float64 `json:"robustnessScore"`   // geomMean - stdDev
}
```

### RobustnessScore の定義

```
RobustnessScore = GeomMeanReturn - ReturnStdDev
```

解釈:
- 全期間安定してプラスなら `geomMean > 0, stdDev 小` → スコア大
- 1 期間だけ大勝して他が負けなら `geomMean 中, stdDev 大` → スコア小
- 単純だが実用十分。**v4 promotion の一次指標**として採用する

**GeomMeanReturn の計算**:
```
GeomMean = ( Π (1 + r_i) )^(1/n) - 1
```
一つでも `r_i <= -1` （破産）が混じると `math.Nan()` を返し、`AllPositive=false` を保証する。

### 実行フロー

1. リクエスト validation（`len(Periods) >= 1`、from <= to、profile 存在、重複 Label 検出）
2. goroutine + errgroup で N 期間を並列実行（最大並列数は環境変数 `BACKTEST_MAX_PARALLEL` デフォルト 4）
3. 各期間の結果は `BacktestRunner.Run` を呼ぶ。既存単一バックテストと同じコードパス
4. 各 `BacktestResult` は `backtest_results` テーブルに個別に保存。`pdca_cycle_id` は共通、`parent_result_id` はリクエスト値を継承
5. まとめは別テーブル `multi_period_results` に保存

### DB マイグレーション

```sql
CREATE TABLE multi_period_results (
    id            TEXT PRIMARY KEY,
    created_at    INTEGER NOT NULL,
    profile_name  TEXT NOT NULL,
    pdca_cycle_id TEXT NOT NULL DEFAULT '',
    hypothesis    TEXT NOT NULL DEFAULT '',
    aggregate_json TEXT NOT NULL,         -- MultiPeriodAggregate のシリアライズ
    period_result_ids TEXT NOT NULL       -- JSON: ["01KP...","01KQ..."] 個別結果への参照
);
CREATE INDEX idx_multi_pdca ON multi_period_results (pdca_cycle_id) WHERE pdca_cycle_id <> '';
```

### API

- `POST /backtest/run-multi` — 上記 `MultiPeriodRequest` を受けて `MultiPeriodResult` を返す
- `GET /backtest/multi-results/:id` — 保存済みまとめを取得
- `GET /backtest/multi-results?profileName=X&pdcaCycleId=Y` — 一覧（フィルタ可）

### CLI

```bash
cat periods.json <<'EOF'
[
  {"label":"1yr","from":"2025-04-01","to":"2026-03-31"},
  {"label":"2yr","from":"2024-04-01","to":"2026-03-31"},
  {"label":"3yr","from":"2023-04-01","to":"2026-03-31"}
]
EOF

go run ./cmd/backtest multi \
  --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv \
  --periods periods.json \
  --pdca-cycle-id 2026-05-01_cycle01 \
  --hypothesis "..."
```

CLI は `multi` サブコマンドを新設（既存 `run` / `optimize` / `refine` と並列）。

### Frontend

新ページ `/backtest/multi`:
- 複数期間一括実行フォーム（profile 選択 + 期間 JSON 入力）
- 実行結果: 横並びテーブル（列 = 期間、行 = 指標）＋ Aggregate カード
- 一覧ビュー: multi-period results を profile 別にソート、`RobustnessScore` 列でランキング

## テスト計画

### Unit

1. `computeAggregate` — 3 期間の入力（+10% / -5% / +3%）に対して期待する geomMean / stdDev / allPositive / robustness
2. 破産ケース — Return = -1.0 が含まれるときに NaN / AllPositive=false を返す
3. Request validation — 期間 0 件 / from > to / Label 重複 / 無効 profile すべて 400

### Integration

1. `runner_test.go` に 1 ケース追加: 3 期間を指定し、各期間の個別結果が DB に保存され、まとめも別テーブルに保存される
2. 並列性能 — 4 期間並列で実行時間がほぼ (最長期間 × 1.x) 以内（マシン負荷で 2x 許容）
3. API test: post-then-get で multi-period-results が正しく返る

### E2E

1. CLI test: `multi` サブコマンドで JSON 出力が得られる

## DoD（as-built）

実装済み項目（PR #111 想定）:

- [x] Unit: `ComputeAggregate` を 7 ケースでカバー（3 positive / mixed / ruin / ruin below -1 / empty / single / worst-drawdown = MAX）
- [x] Unit: `MultiPeriodRunner` の orchestration を 5 ケースでカバー（並列 assembly / empty periods / duplicate labels / empty label / period error propagation）
- [x] Integration: `MultiPeriodResultRepository` の round-trip、FindByID missing、List filter（profile/cycle/no filter）
- [x] Handler tests 5 本: 503 when multi repo missing / 400 empty periods / List filter plumbing / GetMultiResult NotFound / GetMultiResult OK
- [x] `migrations_test` に `multi_period_results` テーブルを加えても冪等性維持
- [x] `go test ./... -race -count=1` 全緑
- [x] docker e2e: production profile × 1yr/2yr/3yr で envelope + 3 per-period が保存され、`/backtest/multi-results/:id` が breakdown 込みで rehydrate されることを確認
- [x] PR 本文: production profile を 1yr/2yr/3yr の 3 期間で回した結果を貼付

### フォローアップ（別 PR）

- CLI `multi` サブコマンド — `cmd/backtest/main.go` への追加。MVP では API で十分（PDCA チャレンジ中は curl で全部回した）ため、実需が出てから実装
- Frontend `/backtest/multi` ページ — `/api/v1/backtest/run-multi` と `/api/v1/backtest/multi-results` を叩く UI。BE 側が安定してから FE 作業

## ロールバック

- `multi_period_results` テーブル削除は `DROP TABLE` で可
- 既存 `POST /backtest/run` は触らないので影響範囲が隔離されている

## 備考

- この PR により、後続 PR-6 (ADX) / PR-12 (ATR trailing) の効果測定は 1 リクエストで済む
- `RobustnessScore` は簡易指標。walk-forward (PR-13) マージ後は後者の OOS スコアの方が信頼できる。両方 Frontend に並べる
