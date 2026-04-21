# PR-13: Walk-forward 最適化

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: C（エンジン側機能）
**Stacked PR 順序**: #6 / 6（PR-6 の上）
**見積もり**: 3 日

## 動機

v3 は 1yr 学習で +7.3% を達成したが、2025Q1 / 2025Q3 で負けるなどレジーム依存の傾向があり「過学習」のシグナル。**Walk-forward optimization (WFO)** は学術・実務両面で過学習対策の標準手法で、in-sample (IS) で最適化 → out-of-sample (OOS) で検証 → 期間をスライドして繰り返し、各窓の OOS 成績を集約する。

**WFO の OOS 平均スコアが本当の実戦性能**。これを基盤に据えれば v4 以降は誤った最適化に時間を溶かさなくなる。

## 対象外

- 遺伝的アルゴリズム等の高度最適化（今回は grid search）
- 並列ジョブスケジューラ（単一プロセスの goroutine 並列で十分）
- profile 自動生成（grid から選ばれた best parameter を profile に書き戻す機能は将来）

## 仕様

### 基本パラメータ

```
InSampleMonths     = 6   # デフォルト
OutOfSampleMonths  = 3
StepMonths         = 3   # 次の窓へのステップ
```

総期間 3 年（36 ヶ月）で IS=6/OOS=3/step=3 なら、窓数 = (36 - 6 - 3) / 3 + 1 = 10 窓。

### データモデル

```go
// usecase/backtest/walkforward.go
type WalkForwardRequest struct {
    BaseProfile      string                 `json:"baseProfile"`      // optimizer の起点 profile
    Data             string                 `json:"data"`
    DataHtf          string                 `json:"dataHtf"`
    From             string                 `json:"from"`
    To               string                 `json:"to"`
    InSampleMonths   int                    `json:"inSampleMonths"`
    OutOfSampleMonths int                   `json:"outOfSampleMonths"`
    StepMonths       int                    `json:"stepMonths"`
    InitialBalance   float64                `json:"initialBalance"`
    TradeAmount      float64                `json:"tradeAmount"`
    ParameterGrid    []ParameterOverride    `json:"parameterGrid"`    // grid 1 点
    Objective        string                 `json:"objective"`        // "return" | "sharpe" | "profit_factor" | "robustness"
    PdcaCycleId      string                 `json:"pdcaCycleId,omitempty"`
}

type ParameterOverride struct {
    Path   string      `json:"path"`    // 例 "strategy_risk.stop_loss_percent"
    Values []float64   `json:"values"`  // 例 [3, 5, 6, 8]
}

type WalkForwardResult struct {
    ID          string            `json:"id"`
    CreatedAt   int64             `json:"createdAt"`
    BaseProfile string            `json:"baseProfile"`
    Windows     []WalkForwardWindow  `json:"windows"`
    AggregateOOS MultiPeriodAggregate `json:"aggregateOOS"`  // PR-2 で定義した型を再利用
}

type WalkForwardWindow struct {
    WindowIndex    int        `json:"windowIndex"`
    InSampleFrom   int64      `json:"inSampleFrom"`
    InSampleTo     int64      `json:"inSampleTo"`
    OOSFrom        int64      `json:"oosFrom"`
    OOSTo          int64      `json:"oosTo"`
    BestParameters map[string]float64 `json:"bestParameters"`
    ISResult       BacktestResult  `json:"isResult"`
    OOSResult      BacktestResult  `json:"oosResult"`
    ParameterGridResults []struct {
        Params map[string]float64 `json:"params"`
        ISSummary BacktestSummary `json:"isSummary"`
    } `json:"parameterGridResults"`
}
```

### Grid 展開

`ParameterGrid` は直積を展開。例:
```
[
  {"path": "strategy_risk.stop_loss_percent", "values": [3, 5, 6]},
  {"path": "signal_rules.trend_follow.rsi_buy_max", "values": [60, 62, 65]}
]
```
→ 3 × 3 = 9 組合せ。上限 `MAX_GRID_SIZE = 100`（環境変数で可変）超過は 400。

### 窓ごとの処理

擬似コード:
```
for each window:
    best_score = -inf
    best_params = nil
    for each param_set in grid:
        profile = applyOverrides(base_profile, param_set)
        is_result = runBacktest(profile, window.IS)
        score = selectByObjective(is_result.Summary, request.Objective)
        if score > best_score:
            best_score = score
            best_params = param_set
    best_profile = applyOverrides(base_profile, best_params)
    oos_result = runBacktest(best_profile, window.OOS)
```

全窓実行後、`oos_result.Summary.TotalReturn` を集めて `MultiPeriodAggregate` を計算。

### Objective 関数

```go
func selectByObjective(s BacktestSummary, obj string) float64 {
    switch obj {
    case "return":          return s.TotalReturn
    case "sharpe":          return s.SharpeRatio
    case "profit_factor":   return s.ProfitFactor
    case "robustness":
        // 内部で in-sample を 2 分割し、両半分の Return 幾何平均 - std
        // （実装詳細は PR 内で検討）
    default: return s.TotalReturn
    }
}
```

### 並列性

- 窓 × grid 点 の総実行数は容易に数百になる
- `errgroup` + `semaphore` で並列上限 `BACKTEST_MAX_PARALLEL` まで（デフォルト 4）
- 窓内の grid は独立に走らせて OK、ただし窓は順序依存なし

### DB

```sql
CREATE TABLE walk_forward_results (
    id                 TEXT PRIMARY KEY,
    created_at         INTEGER NOT NULL,
    base_profile       TEXT NOT NULL,
    pdca_cycle_id      TEXT NOT NULL DEFAULT '',
    request_json       TEXT NOT NULL,       -- WalkForwardRequest
    result_json        TEXT NOT NULL,       -- WalkForwardResult（個別 BacktestResult を除く要約）
    aggregate_oos_json TEXT NOT NULL
);

CREATE INDEX idx_wf_pdca ON walk_forward_results (pdca_cycle_id) WHERE pdca_cycle_id <> '';
```

個別 `BacktestResult` は `backtest_results` に保存。WFO 結果の `ISResult.ID / OOSResult.ID` で参照。

### API

- `POST /backtest/walk-forward` — 上記を受けて `WalkForwardResult` を返す（長時間実行、最大 5 分想定）
- `GET /backtest/walk-forward/:id` — 取得
- `GET /backtest/walk-forward?baseProfile=X&pdcaCycleId=Y` — 一覧

タイムアウト: サーバ側 10 分、クライアント 15 分程度を推奨。

### CLI

```bash
go run ./cmd/backtest walk-forward \
  --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv \
  --from 2023-04-01 --to 2026-03-31 \
  --in 6 --oos 3 --step 3 \
  --grid grid.json \
  --objective return \
  --pdca-cycle-id 2026-05-xx_cycleNN
```

### Frontend

新ページ `/backtest/walk-forward`:
- 実行フォーム（base profile 選択 + grid JSON エディタ + in/oos/step slider）
- 結果表示: 窓別 OOS Return の折れ線 + Aggregate カード + best parameter 頻度分布

## テスト計画

### Unit

1. Window 分割: `(from=2023-04-01, to=2026-03-31, in=6, oos=3, step=3)` で 10 窓、各窓の IS/OOS 境界が一致
2. Grid 展開: 直積が期待通り
3. Objective 関数: 各モードで正しい field が選ばれる
4. エッジケース: `to - from < in + oos` で window=0、400 エラー

### Integration

5. 小スケール WFO: 3 窓、2 × 2 grid で全窓が走り AggregateOOS が返る
6. 並列性: 4 並列で所要時間が直列の 1/2 〜 1/3 程度
7. DB 往復: Save → Get で結果が一致

## DoD

- [ ] Unit 4 本 + Integration 3 本 = **7 本** passing
- [ ] CLI E2E で WFO ジョブが完走
- [ ] Frontend ページ実装 + `pnpm test` pass
- [ ] PR 本文: **現 production の 3 年 WFO 結果** を貼付（窓別 OOS + aggregate）
- [ ] PR 本文: **v3 の過学習度評価** — 1yr 学習時の +7.3% と、同期間を WFO 評価した場合の差を報告

## ロールバック

- テーブル追加のみ、既存機能は無変更
- PR revert で影響最小

## 備考

- 将来の拡張: 遺伝的アルゴリズム / ベイズ最適化は WFO フレームの上に差し込める構造にする
- 実行中進捗表示は SSE / websocket がベストだが MVP ではポーリング（`GET` が部分結果を返す）
- 本 PR のマージ後、`docs/pdca/agent-guide.md` §4 に「Walk-forward を使った採用判定フロー」を追加する
