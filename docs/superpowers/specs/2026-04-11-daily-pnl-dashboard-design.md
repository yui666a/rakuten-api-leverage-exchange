# Daily PnL Dashboard — Design Spec

- **Date**: 2026-04-11
- **Status**: Draft (awaiting implementation plan)
- **Owner**: yui666a

## Problem

ダッシュボードの「日次損益」カードが実態と噛み合っていない。

- 現状: `GET /api/v1/pnl` が `RiskManager.dailyLoss` を返し、フロントはそれを反転して表示している (`frontend/src/routes/index.tsx:40`)。
- `RiskManager.dailyLoss` は `MaxDailyLoss` 超過で Bot を止めるためのリスク制限用カウンタであり、**ストップロス発動時だけ**更新される (`backend/cmd/pipeline.go:379`)。
- 結果として、手動クローズ (`POST /api/v1/positions/:id/close`) や別セッションによる約定は一切反映されず、本日 -¥6 の確定損があっても画面は `¥0` のまま表示される。
- `dailyLoss` は片方向 (損失のみ) のカウンタでもあり、利益は意味的にも集計できない。

「日次損益」を実態どおり **全通貨ペア合算の実現損益 (JST 当日分) + 未実現損益 (全保有ポジションの含み損益)** として計算し、画面に表示する。

## Non-Goals

- `RiskManager.dailyLoss` の廃止・削除 (リスク停止判定や設定 API と連動しているため互換性のため残す)。
- 銘柄ごとの日次損益ブレークダウン表示。
- ローカル `tradeHistoryRepo` と楽天 API の merge (楽天を唯一のソースとする)。
- WebSocket push 更新 (既存の 10 秒 polling で十分)。
- 履歴的な損益グラフや過去日比較。

## Architecture

新しいユースケース `usecase.DailyPnLCalculator` を追加し、既存 `RiskHandler.GetPnL` から呼び出してレスポンスに埋め込む。`RiskManager` には一切手を入れない (責務分離)。

```
GET /api/v1/pnl
    ↓
RiskHandler.GetPnL  (既存、拡張)
    ├── riskMgr.GetStatus() → balance, dailyLoss, totalPosition, tradingHalted
    └── dailyPnLCalc.Compute(ctx) → DailyPnL{realized, unrealized, total, stale, computedAt}
    ↓
JSON マージして返却
```

### `DailyPnLCalculator`

場所: `backend/internal/usecase/daily_pnl.go`

```go
type DailyPnL struct {
    Realized   float64 `json:"realized"`
    Unrealized float64 `json:"unrealized"`
    Total      float64 `json:"total"`
    Stale      bool    `json:"stale"`
    ComputedAt int64   `json:"computedAt"` // unix seconds
}

type Clock interface { Now() time.Time }

type DailyPnLCalculator struct {
    rest  rakutenClient   // interface for testability
    clock Clock
    cache atomic.Pointer[cachedPnL]
    group singleflight.Group
    ttl   time.Duration   // 10s
}

type rakutenClient interface {
    GetSymbols(ctx context.Context) ([]entity.Symbol, error)
    GetMyTrades(ctx context.Context, symbolID int64) ([]entity.MyTrade, error)
    GetPositions(ctx context.Context, symbolID int64) ([]entity.Position, error)
}

func (c *DailyPnLCalculator) Compute(ctx context.Context) (DailyPnL, error)
```

### フロー

1. キャッシュ確認: `age < ttl` ならそのまま返す。
2. `singleflight.Do("pnl", ...)` で同時多発リクエストを 1 つに収束。
3. `GetSymbols` で有効銘柄を取得。
4. 各銘柄に対して `GetMyTrades` と `GetPositions` を**並列**で叩く (`errgroup` もしくは `sync.WaitGroup`)。
5. `MyTrade.CreatedAt >= todayStartJST.UnixMilli()` のものだけ残し、`closeTradeProfit` を合算 → `realized`。
6. 全銘柄の `Position.FloatingProfit` を合算 → `unrealized`。
7. `total = realized + unrealized`
8. キャッシュに書き込んで返却。

### JST 境界

```go
jst := time.FixedZone("JST", 9*60*60)
now := c.clock.Now().In(jst)
todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
cutoffMillis := todayStart.UnixMilli()
```

`backend/cmd/pipeline.go:500` の `restoreRiskState` と同じ JST 固定ゾーンを使うことで境界定義を一貫させる。

### エラー処理

- 個別銘柄の失敗 → その銘柄ぶんは 0 として扱い、`slog.Warn` でログを残し、レスポンスの `stale` を `true` にする。
- **全銘柄失敗** → `Compute` がエラーを返す。ハンドラは 500 を返す。
- キャッシュが生きている間に背面更新でエラーになった場合 → 古い値を `stale: true` で返す。

### キャッシュ戦略

- TTL: **10 秒** (フロントの `usePnl.refetchInterval` と同じ `10_000ms`、`frontend/src/hooks/usePnl.ts:8`)
- `singleflight.Group` で同時リクエストを 1 コールに収束。
- 1 リクエスト当たりの楽天 API コール: キャッシュミス時 `1 (GetSymbols) + N (GetMyTrades) + N (GetPositions)`、現在有効な 9 銘柄で **19 コール**。キャッシュヒット時は **0 コール**。
- `GetSymbols` の個別長期キャッシュは YAGNI で見送り。問題が出たら個別に伸ばす。

## API スキーマ (後方互換)

### Before

```json
{
  "balance": 9998,
  "dailyLoss": 0,
  "totalPosition": 0,
  "tradingHalted": false
}
```

### After

```json
{
  "balance": 9998,
  "dailyLoss": 0,
  "totalPosition": 0,
  "tradingHalted": false,
  "dailyPnl": {
    "realized": -6,
    "unrealized": 0,
    "total": -6,
    "stale": false,
    "computedAt": 1775916700
  }
}
```

- 既存フィールドは**削除しない**。`dailyLoss` は `RiskManager` のリスク制限用カウンタとして引き続き意味を持つ (Bot 停止判定に使用)。
- フロントは `dailyLoss` 参照をやめ、`dailyPnl.total` のみを表示に使う。

## Frontend 変更

### `frontend/src/lib/api.ts`

`PnlResponse` 型に `dailyPnl` を追加:

```ts
export type DailyPnLBreakdown = {
  realized: number
  unrealized: number
  total: number
  stale: boolean
  computedAt: number
}

export type PnlResponse = {
  balance: number
  dailyLoss: number
  totalPosition: number
  tradingHalted: boolean
  dailyPnl: DailyPnLBreakdown
}
```

### `frontend/src/routes/index.tsx`

```tsx
// before
const dailyPnl = pnl ? -pnl.dailyLoss : null
const dailyPnlLabel =
  dailyPnl === null
    ? '—'
    : dailyPnl === 0
      ? '¥0'
      : `¥${dailyPnl.toLocaleString()}`

// after
const dailyPnlTotal = pnl?.dailyPnl?.total ?? null
const dailyPnlStale = pnl?.dailyPnl?.stale ?? false
const dailyPnlLabel =
  dailyPnlTotal === null
    ? '—'
    : `${dailyPnlTotal >= 0 ? '' : '-'}¥${Math.abs(dailyPnlTotal).toLocaleString()}${dailyPnlStale ? '*' : ''}`
```

色分け: `dailyPnlTotal < 0` → `text-accent-red`、それ以外 → `text-accent-green`。
`*` (stale マーカー) は `title` 属性でホバー説明を入れる。

## Test Plan

### Backend (`usecase/daily_pnl_test.go`)

Fake `rakutenClient` と fake `Clock` を注入してテストする。

1. **正常系**: 今日の trades と positions から `realized + unrealized` を正しく計算する (複数銘柄含む)。
2. **JST 境界**: 昨日 23:59:59.999 JST の trade は除外、今日 00:00:00.000 JST の trade は含む (`createdAt` ミリ秒)。
3. **キャッシュ**: 10 秒以内の再呼び出しで楽天 API コール数が増えないこと (fake クライアントのコールカウントで検証)。
4. **TTL 経過**: `clock.Now()` を TTL 超過に進めて再呼び出しすると新しくフェッチが走ること。
5. **singleflight**: goroutine 100 本から同時に `Compute` しても楽天 API 呼び出しは 1 セット分で済むこと。
6. **個別銘柄失敗**: 1 銘柄の `GetPositions` がエラーを返したケースで、残りの銘柄から集計した結果と `stale: true` が返ること。
7. **全銘柄失敗**: 全銘柄でエラー → `Compute` がエラーを返すこと。
8. **空**: trades も positions も空 → `total: 0, stale: false`。

### Backend (`interfaces/api/api_test.go` への追記)

9. `GET /api/v1/pnl` のレスポンス JSON に `dailyPnl` ブロックが含まれ、既存フィールド (`balance`, `dailyLoss`, `totalPosition`, `tradingHalted`) がそのまま返ること (後方互換性)。

### Frontend

既存の index route テストがある場合は `dailyPnl.total` を `pnl` に差し込んだモックで、表示文字列と色クラスが期待通りになることを検証する。既存テストが無い場合はスコープ外とする (既存状況に合わせる)。

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| 19 コール/リクエストで楽天 API のレート制限に当たる | 10 秒 TTL + singleflight で実質負荷を抑える。タブ複数開きでも 10 秒 1 セット |
| `GetMyTrades` のページング未考慮 | 楽天 API は最近のトレードから返るため、「今日分」に対しては 1 ページで足りる。ただし実装時に既存 `tradeHandler.GetAllTrades` の実装を参照し、ページング仕様を確認する (実装プラン側で担保) |
| 銘柄リストが変化した瞬間に古い銘柄の取引が取りこぼされる | `GetSymbols` を都度 (= 10 秒毎) 取得するので最長 10 秒で追随 |
| `MyTrade.Profit` vs `MyTrade.CloseTradeProfit` の誤選択 | 「決済で発生した確定損益」は `CloseTradeProfit`。新規建ては `0` で入るため合算して問題なし (`entity/trade.go:30`) |
| キャッシュ下で手動クローズ直後に画面が古いまま見える | TTL 10 秒の範囲内では仕様。急ぎなら TTL を短くするか無効化オプションを検討 (将来 issue) |

## Out of Scope / Future Work

- 銘柄別の損益ブレークダウン UI
- 今週/今月の PnL 集計
- `dailyLoss` (リスク制限用) の命名変更または削除
- ローカル trade 履歴との照合
- キャッシュ戦略の細分化 (`GetSymbols` だけ長期キャッシュ等)
