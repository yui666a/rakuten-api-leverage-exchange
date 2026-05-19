# 保有ポジション建値水平線 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ローソク足チャートに、現在保有中ポジションの建値を BUY=緑 / SELL=赤 の dashed 水平線 + 右端ラベルで常時表示する。

**Architecture:** `usePositions(symbolId)` の結果を `CandlestickChart` に props で渡し、`positionStatus === 'OPEN'` のものを `IPanePrimitive` ベースの新 primitive (`PositionPriceLinePrimitive`) で pane 最上層に直接描画する。既存の `TradeMarkerPrimitive` (PR #268 で追加された約定縦線) と独立に共存する。

**Tech Stack:** React 19, TypeScript, lightweight-charts v5 (`IPanePrimitive` API), TanStack Query, vite, vitest, pnpm。

**Spec:** `docs/superpowers/specs/2026-05-20-position-price-line-design.md`

---

## File Structure

| ファイル | 種類 | 責務 |
|---|---|---|
| `frontend/src/lib/api.ts` | Modify | `Position` 型に `positionStatus: 'OPEN' \| 'CLOSED'` を追加 |
| `frontend/src/components/CandlestickChart.tsx` | Modify | `positions?` prop の受け口、`PositionPriceLinePrimitive` クラス、描画 effect、cleanup |
| `frontend/src/routes/index.tsx` | Modify | 既存の `positions` を `<CandlestickChart>` に props として渡す |

新規ファイルは作らない。`PositionPriceLinePrimitive` は `CandlestickChart.tsx` 内に同居させる (既存の `TradeMarkerPrimitive` や `KumoFillPrimitive` と同じパターン)。

---

## Task 1: `Position` 型に `positionStatus` を追加する

**Why:** 楽天 API (`GET /api/v1/positions`) は `positionStatus: "OPEN" | "CLOSED"` を返しているが、フロントの型定義に欠落している (`frontend/src/lib/api.ts:71-78`)。後続タスクで `p.positionStatus === 'OPEN'` フィルタを書くため、最初に型を直す。

**Files:**
- Modify: `frontend/src/lib/api.ts:71-78`

- [ ] **Step 1: 型を編集する**

`frontend/src/lib/api.ts` の `Position` 型を次のように変更:

```typescript
export type Position = {
  id: number
  symbolId: number
  positionStatus: 'OPEN' | 'CLOSED'
  orderSide: 'BUY' | 'SELL'
  price: number
  remainingAmount: number
  floatingProfit: number
}
```

- [ ] **Step 2: 型チェック**

Run: `cd frontend && pnpm tsc --noEmit`
Expected: PASS (型エラーなし、`positionStatus` を未指定で生成している既存箇所があれば露呈する)

もし `positionStatus` を欠いた `Position` リテラルを作っているテスト/モックがあれば、`positionStatus: 'OPEN'` を追加する。`grep -rn "orderSide: 'BUY'" frontend/src` で候補を探せる。

- [ ] **Step 3: コミット**

```bash
git add frontend/src/lib/api.ts
git commit -m "$(cat <<'EOF'
refactor(frontend): Position 型に positionStatus を追加

GET /api/v1/positions は positionStatus を返しているが、フロントの
Position 型に欠落していた。保有中ポジションの建値水平線描画の前提
として型を直す。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `PositionPriceLinePrimitive` クラスを追加する

**Why:** lightweight-charts の `IPanePrimitive` を使って pane に直接 canvas 描画する。既存の `TradeMarkerPrimitive` (`CandlestickChart.tsx:436-481` 付近) と同じパターンを踏襲。

**Files:**
- Modify: `frontend/src/components/CandlestickChart.tsx` (新クラスを `TradeMarkerPrimitive` の直後に追加)

- [ ] **Step 1: 型と定数を追加**

`CandlestickChart.tsx` の既存の `TradeMarkerPrimitive` クラスの直後 (≒ `class TradeMarkerPrimitive` の閉じ `}` の次の空行) に、以下を追記:

```typescript
type PositionPriceLine = { price: number; side: 'BUY' | 'SELL'; amount: number }

const POSITION_BUY_COLOR = 'rgba(0, 212, 170, 0.85)'
const POSITION_SELL_COLOR = 'rgba(255, 71, 87, 0.85)'
const POSITION_LINE_WIDTH = 1
const POSITION_DASH = [6, 4] as const
const POSITION_LABEL_FONT = '11px ui-monospace, SFMono-Regular, Menlo, monospace'
const POSITION_LABEL_BG = 'rgba(0, 0, 0, 0.65)'
const POSITION_LABEL_PADDING_X = 5
const POSITION_LABEL_PADDING_Y = 2
const POSITION_LABEL_GAP = 6 // ライン右端からラベルまでの余白
```

- [ ] **Step 2: Renderer クラスを追加**

上記定数の直後に:

```typescript
class PositionPriceLineRenderer implements IPrimitivePaneRenderer {
  private _data: PositionPriceLine[] = []
  private _series: ISeriesApi<'Candlestick'> | null = null

  update(data: PositionPriceLine[], series: ISeriesApi<'Candlestick'>) {
    this._data = data
    this._series = series
  }

  draw(target: CanvasRenderingTarget2D): void {
    const series = this._series
    if (!series || this._data.length === 0) return

    target.useMediaCoordinateSpace(({ context: ctx, mediaSize }) => {
      ctx.save()
      ctx.lineWidth = POSITION_LINE_WIDTH
      ctx.font = POSITION_LABEL_FONT
      ctx.textBaseline = 'middle'

      for (const d of this._data) {
        const y = series.priceToCoordinate(d.price)
        if (y === null) continue
        const py = Math.round(y) + 0.5
        const color = d.side === 'BUY' ? POSITION_BUY_COLOR : POSITION_SELL_COLOR

        // Dashed horizontal line across the full pane width.
        ctx.strokeStyle = color
        ctx.setLineDash([...POSITION_DASH])
        ctx.beginPath()
        ctx.moveTo(0, py)
        ctx.lineTo(mediaSize.width, py)
        ctx.stroke()
        ctx.setLineDash([])

        // Right-aligned label chip: "8891.9 BUY 0.9".
        const text = `${d.price.toFixed(1)} ${d.side} ${d.amount}`
        const metrics = ctx.measureText(text)
        const textW = metrics.width
        const textH = 12 // 11px font + small padding
        const boxW = textW + POSITION_LABEL_PADDING_X * 2
        const boxH = textH + POSITION_LABEL_PADDING_Y * 2
        const boxX = mediaSize.width - boxW - POSITION_LABEL_GAP
        const boxY = py - boxH / 2

        ctx.fillStyle = POSITION_LABEL_BG
        ctx.fillRect(boxX, boxY, boxW, boxH)
        ctx.fillStyle = color
        ctx.fillText(text, boxX + POSITION_LABEL_PADDING_X, py)
      }
      ctx.restore()
    })
  }
}
```

- [ ] **Step 3: Primitive クラスを追加**

Renderer の直後に:

```typescript
class PositionPriceLinePrimitive implements IPanePrimitive<Time> {
  private _renderer = new PositionPriceLineRenderer()
  private _data: PositionPriceLine[] = []
  private _series: ISeriesApi<'Candlestick'>
  private _requestUpdate: (() => void) | null = null
  private _paneViews: IPanePrimitivePaneView[]

  constructor(series: ISeriesApi<'Candlestick'>) {
    this._series = series
    const renderer = this._renderer
    this._paneViews = [{
      zOrder: () => 'top' as const,
      renderer: () => renderer,
    }]
  }

  setData(data: PositionPriceLine[]) {
    this._data = data
    this._updateRenderer()
    this._requestUpdate?.()
  }

  attached(param: PaneAttachedParameter<Time>) {
    this._requestUpdate = param.requestUpdate
    this._updateRenderer()
    this._requestUpdate?.()
  }

  detached() {
    this._requestUpdate = null
  }

  updateAllViews() {
    this._updateRenderer()
  }

  paneViews(): readonly IPanePrimitivePaneView[] {
    return this._paneViews
  }

  private _updateRenderer() {
    this._renderer.update(this._data, this._series)
  }
}
```

- [ ] **Step 4: 型チェック**

Run: `cd frontend && pnpm tsc --noEmit`
Expected: PASS (新クラスは未使用なので「declared but never used」警告は出ないはず — TS の `noUnusedLocals` はファイルスコープのトップレベル class には適用されない / プロジェクト設定次第)

警告が出る場合は次タスクで使用するので無視可。ただしエラーになるなら設定を確認する: `grep -n "noUnused" frontend/tsconfig*.json`。

- [ ] **Step 5: コミット (WIP として)**

このタスク単体ではクラスは未使用のため、Task 3 とまとめてコミットしたい。**ここではコミットせず Task 3 へ進む**。

---

## Task 3: `CandlestickChart` に `positions` prop と描画 effect を追加する

**Why:** 親から `positions` を受け取り、`PositionPriceLinePrimitive` に流す。

**Files:**
- Modify: `frontend/src/components/CandlestickChart.tsx`
  - props 型 (`type CandlestickChartProps`)
  - 関数引数の分割代入
  - `tradeMarkerRef` の直後に `positionPriceLineRef` 追加
  - cleanup ブロック (`return () => { ... }` 内 `tradeMarkerRef.current = null` の直後)
  - 新規 `useEffect` を「Render vertical lines at production trade times」 effect の直後に追加

- [ ] **Step 1: import 確認**

ファイル先頭の `lightweight-charts` from import に `IPanePrimitive`, `IPanePrimitivePaneView`, `PaneAttachedParameter` がすでに含まれていることを確認 (Task 2 で使ったため)。含まれていなければ追加する。`Position` の import は不要 (`api.ts` の `Position` 型を直接使う場合は次で追加)。

- [ ] **Step 2: `Position` 型を import**

`CandlestickChart.tsx` 冒頭のローカル import 群に追加:

```typescript
import type { Position } from '../lib/api'
```

(配置は他の `import type` の隣、たとえば `import { useTradeHistory } from '../hooks/useTradeHistory'` の近く)

- [ ] **Step 3: Props 型を変更**

`CandlestickChartProps` を探す (`grep -n "CandlestickChartProps" frontend/src/components/CandlestickChart.tsx`)。次のように変更:

```typescript
type CandlestickChartProps = {
  symbolId: number
  positions?: Position[]
}
```

- [ ] **Step 4: 関数シグネチャを変更**

`export function CandlestickChart({ symbolId }: CandlestickChartProps)` を:

```typescript
export function CandlestickChart({ symbolId, positions }: CandlestickChartProps) {
```

- [ ] **Step 5: ref を追加**

既存の `const tradeMarkerRef = useRef<TradeMarkerPrimitive | null>(null)` の直後に:

```typescript
  const positionPriceLineRef = useRef<PositionPriceLinePrimitive | null>(null)
```

- [ ] **Step 6: cleanup に追加**

`CandlestickChart.tsx:590-602` の cleanup ブロック内、`tradeMarkerRef.current = null` の直後に:

```typescript
      positionPriceLineRef.current = null
```

- [ ] **Step 7: 描画 effect を追加**

「Render vertical lines at production trade times」 effect (約定縦線の useEffect) の閉じ `}, [trades, tradeLinesVisible, candles])` の直後に、新規 useEffect を追加:

```typescript
  // Render horizontal price lines for each OPEN position (BUY=green, SELL=red).
  // Independent from the trade-marker overlay above: held positions matter even
  // when the venue's /cfd/trade window has expired and `trades` is empty.
  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const pane = chart.panes()[0]
    if (!pane) return
    const series = seriesRef.current
    if (!series) return

    const openPositions = (positions ?? []).filter(
      (p) => p.positionStatus === 'OPEN' && p.remainingAmount > 0,
    )

    if (openPositions.length > 0) {
      let prim = positionPriceLineRef.current
      if (!prim) {
        prim = new PositionPriceLinePrimitive(series)
        pane.attachPrimitive(prim)
        positionPriceLineRef.current = prim
      }
      prim.setData(openPositions.map((p) => ({
        price: p.price,
        side: p.orderSide,
        amount: p.remainingAmount,
      })))
    } else {
      const prim = positionPriceLineRef.current
      if (prim) {
        pane.detachPrimitive(prim)
        positionPriceLineRef.current = null
      }
    }
  }, [positions])
```

- [ ] **Step 8: 型チェック**

Run: `cd frontend && pnpm tsc --noEmit`
Expected: PASS (`positions` prop が呼び出し側で未指定でも `optional` なので型エラーは出ないが、Task 4 で必ず渡す)

- [ ] **Step 9: ビルドが通ることを確認**

Run: `cd frontend && pnpm build`
Expected: PASS (lightweight-charts API 型と整合していれば成功)

ビルドが失敗したら、`pane.attachPrimitive(prim)` の戻り型エラーや `priceToCoordinate` 引数の型ミスマッチを疑う。`series.priceToCoordinate(d.price)` は `number | null` を返すので `null` チェック済み。

- [ ] **Step 10: コミット**

```bash
git add frontend/src/components/CandlestickChart.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): ローソク足チャートに保有ポジション建値の水平線を追加

PR #268 の約定縦線とは別に、現在 OPEN のポジションごとに BUY=緑 /
SELL=赤 の dashed 水平線 + 右端ラベル ("8891.9 BUY 0.9") を pane に
直接描画する。楽天 /cfd/trade が短期保持しか返さず縦線が出ない時間
帯でも、保有ポジションの建値は常に視認できる。

- IPanePrimitive ベースで TradeMarkerPrimitive と同じパターン
- usePositions(symbolId) の結果を props 経由で受け取る
- positionStatus==='OPEN' かつ remainingAmount>0 のみ対象
- ポジション決済 (OPEN->CLOSED 遷移) で 30s 以内に消える

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `routes/index.tsx` から `positions` を渡す

**Why:** 親で取得済みの `positions` を子に流す。

**Files:**
- Modify: `frontend/src/routes/index.tsx:132`

- [ ] **Step 1: JSX を編集**

`frontend/src/routes/index.tsx:132` の `<CandlestickChart symbolId={symbolId} />` を:

```tsx
          <CandlestickChart symbolId={symbolId} positions={positions} />
```

(`positions` は `routes/index.tsx:31` で `const { data: positions } = usePositions(symbolId)` として取得済み、line 138 でも使用中)

- [ ] **Step 2: 型チェック**

Run: `cd frontend && pnpm tsc --noEmit`
Expected: PASS

- [ ] **Step 3: 動作確認 (手動)**

Run: `docker compose up --build -d frontend backend`
ブラウザで `http://localhost:33000/` を開き、現在保有中の LTC ポジション (BUY @ 8891.9, 0.9) に対して以下を確認:

1. 8891.9 の高さに緑の dashed 水平線が出る
2. 右端に `8891.9 BUY 0.9` ラベルが緑色で出る
3. PR #268 の約定縦線とは独立に表示される (`約定線` トグル OFF にしても水平線は残る)
4. ローソク・MA・BB・Ichimoku などの既存系列を線が見づらく邪魔しない

スクリーンショットがあれば PR にも添付する。

- [ ] **Step 4: コミット**

```bash
git add frontend/src/routes/index.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): index ルートから CandlestickChart に positions を渡す

保有ポジションの建値水平線描画 (前コミット) に必要な props を配線。

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: 受け入れチェックリストと PR 作成

**Why:** spec のエッジケースを実機で 1 通り叩いておく。手動 QA で済むスコープのため、自動テストは追加しない (既存 CandlestickChart にも単体テストはなく、canvas 描画は vitest + jsdom で検証不能)。

**Files:**
- なし (検証のみ)

- [ ] **Step 1: 受け入れチェック**

`docs/superpowers/specs/2026-05-20-position-price-line-design.md` の「手動検証チェックリスト」7 項目をすべて踏む:

1. BUY ポジションに緑の dashed 線が建値高さに出る
2. 右端に `8891.9 BUY 0.9` ラベル
3. 既存系列を邪魔しない (α 0.85)
4. `約定線` トグル OFF でも建値線は残る
5. ズームアウトしたとき価格軸範囲外でも他描画が崩れない
6. ポジション決済後 30s 以内に水平線が消える (今手元のポジションを実際に決済できる状況なら実機確認。決済できない場合は `usePositions` の refetchInterval 動作のみ確認し、後日再検証としてメモ)
7. symbol セレクタで他銘柄に切り替えて表示が入れ替わる

決済まで含めた検証ができない場合は、項目 6 だけ "未確認 / 次回ポジションクローズ時に検証" として PR description に明記する。

- [ ] **Step 2: スクリーンショットを撮る**

ブラウザで建値線が見える状態のスクリーンショットを 1 枚撮り、リポジトリのルートに `position-price-line.png` として保存 (既存の `chart-*.png` と同じ位置)。

- [ ] **Step 3: PR 作成**

Run:

```bash
git push -u origin HEAD
gh pr create --title "feat(frontend): ローソク足チャートに保有ポジション建値の水平線を追加" --body "$(cat <<'EOF'
## Summary
- 現在 OPEN のポジションごとに BUY=緑 / SELL=赤 の dashed 水平線を建値高さに描画
- 右端に \`8891.9 BUY 0.9\` 形式のラベル付き
- PR #268 の約定縦線とは独立。楽天 /cfd/trade が空でも建値は常に視認可能

## Design
- Spec: \`docs/superpowers/specs/2026-05-20-position-price-line-design.md\`
- Plan: \`docs/superpowers/plans/2026-05-20-position-price-line.md\`

## Test plan
- [x] BUY ポジションに緑 dashed 線が建値高さに表示
- [x] 右端に \`{price} {side} {amount}\` ラベル
- [x] 既存系列 (MA/BB/Ichimoku) と視覚的に干渉しない
- [x] \`約定線\` トグル OFF でも水平線は残る (独立)
- [x] ズームアウト時に価格軸外のポジションがあっても他描画が崩れない
- [x] symbol 切り替えで線が入れ替わる
- [ ] ポジション決済→水平線消滅 (次回クローズ時に検証 / 必要に応じて削除)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: PR URL を返す**

`gh pr create` の出力 URL をユーザーに返す。

---

## Self-Review

**Spec coverage:**
- ✅ `Position` 型修正 (positionStatus 追加): Task 1
- ✅ `PositionPriceLinePrimitive` 新設: Task 2
- ✅ props 配線と描画 effect: Task 3 / Task 4
- ✅ 色・線幅・透明度・ラベル書式の具体値: Task 2 Step 1 で定数化
- ✅ `priceToCoordinate === null` の skip: Task 2 Step 2 の Renderer.draw 内
- ✅ Cleanup: Task 3 Step 6
- ✅ 受け入れ確認手順: Task 5 Step 1

**Placeholder scan:** TBD / TODO / "implement later" / 抽象的な「適切に〜」表現なし。

**Type consistency:**
- `PositionPriceLine = { price, side, amount }` (Task 2 Step 1) と `prim.setData(openPositions.map((p) => ({ price: p.price, side: p.orderSide, amount: p.remainingAmount })))` (Task 3 Step 7) が一致
- ref 名は `positionPriceLineRef` で Task 3 全体を通じて統一
- クラス名 `PositionPriceLinePrimitive` も Task 2/3 で統一

**Ambiguity:** なし。色定数は具体的な rgba 値、線幅 1px、dash [6,4]、ラベル 11px monospace、padding 5/2、gap 6 と全て明示済み。

問題なし。実装フェーズへ。
