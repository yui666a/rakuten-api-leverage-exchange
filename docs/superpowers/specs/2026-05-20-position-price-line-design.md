# 保有ポジションの建値水平線をローソク足チャートに表示する

- **Status:** Proposed
- **Date:** 2026-05-20
- **Related:** PR #268 (5003286) — ローソク足チャートに本番約定の縦線を追加

## 背景

PR #268 で、`CandlestickChart` に本番環境の約定タイミングを縦線でオーバーレイ表示する機能を追加した。BUY=緑 / SELL=赤の縦線で、データソースは `useTradeHistory` (楽天 `GET /cfd/trade` のラッパー)。

しかし楽天 API の約定履歴は短期間しか返さないことが運用で判明し、ボットが新規発注を止めて 24h 以上経過すると `/trades` が `[]` を返し、縦線が一切表示されなくなる。一方で「いま自分が保有しているポジションの建値」は最も視認したい情報であり、楽天 API の `GET /cfd/position` (フロントの `usePositions` 経由) からは常に取得できる。

そこで、過去約定の縦線とは別に **保有中ポジションの建値を水平線で表示する**機能を追加する。

## 目的

- 保有中ポジションの建値 (`price`) と現在の価格・指標との位置関係を、ローソク足チャート上で一目で把握できるようにする
- 既存の縦線 (過去約定タイミング) は維持する。両者は別の意味を持つ独立した overlay として共存させる

## 非目的

- TP/SL ライン、ストップトレール、ATR ボラティリティバンドなど、ポジション関連の他の水平線描画 (本タスクのスコープ外、別タスクで議論)
- 過去のクローズ済みポジションの建値表示 (CLOSED は対象外)
- 「建値線」の表示トグルボタンの追加 (保有中ポジションの建値は常時表示すべき重要情報のため)
- `useTradeHistory` / `GET /cfd/trade` 側の改善 (別途、ローカル DB 永続化として B 案を検討)

## 設計

### データフロー

```
routes/index.tsx
  ├ usePositions(symbolId) ─┐
  │                          ▼
  └─ <CandlestickChart symbolId={symbolId} positions={positions} />
                              │
                              ▼
            positionStatus==='OPEN' のみ抽出
                              ▼
        各ポジションに対応する水平線データを生成し
        IPanePrimitive 経由で pane に直接描画
```

`positions` は親 (`routes/index.tsx`) ですでに `usePositions(symbolId)` で取得済みのため、props で `CandlestickChart` に渡す。`CandlestickChart` 内で独立して `usePositions` を呼ぶ案もあったが、props 駆動にすることで「親と同じデータを描画している」ことがコンポーネントの型から明らかになる利点を優先する。

### 影響範囲

| ファイル | 変更内容 |
|---|---|
| `frontend/src/lib/api.ts` | `Position` 型に `positionStatus: 'OPEN' \| 'CLOSED'` を追加 (現状欠落) |
| `frontend/src/routes/index.tsx` | `<CandlestickChart>` に `positions={positions}` を渡す |
| `frontend/src/components/CandlestickChart.tsx` | (a) `positions?: Position[]` を props に追加、(b) `PositionPriceLinePrimitive` クラスを新設、(c) `useEffect` で `positions` を加工して primitive に渡す |

### 描画仕様

- **対象:** `positionStatus === 'OPEN'` かつ `remainingAmount > 0` のポジションのみ
- **線:** 1px、`dashed` (segment 6px / gap 4px)
- **色:** BUY = `rgba(0, 212, 170, 0.85)` / SELL = `rgba(255, 71, 87, 0.85)` (既存の縦線と同じパレット、α だけ若干下げて主役のローソクを邪魔しない)
- **ラベル:** チャート右端に `{price.toFixed(1)} {side} {remainingAmount}` 形式の小さなチップ。背景は `rgba(0,0,0,0.65)`、文字色は線と同色、フォントは 11px monospace
- **price → Y 変換:** `candleSeriesRef.current?.priceToCoordinate(position.price)`。`null` (=価格軸の表示範囲外) のときはその 1 本だけ skip。他の線の描画は継続する
- **同価格の重複:** 現運用では同一銘柄の同一ポジションが価格 1 円単位で重なる可能性は低いが、念のため `price` で集約せず、ポジションごとに 1 本ずつ描画する (ID で区別できるよう将来 hover ラベルを足したい場合に備える)

### 既存の `TradeMarkerPrimitive` との関係

- 別クラス `PositionPriceLinePrimitive` を新設する。役割が「縦線 (時刻)」と「横線 (価格)」で直交しているため、1 クラスに mixin するより分離した方がレンダラとデータ構造がシンプルになる
- どちらも `chart.panes()[0].attachPrimitive(...)` で同じ pane に乗せる
- `zOrder` は `'top'` (縦線と同じ)。両者が交差した場合は描画順 (= 後に attach した方が上) に従う。約定線が後、建値線が先 (= 約定縦線が建値水平線の上に来る) で問題ないため、attach 順序は気にしない

### `useEffect` の構造

新規 `useEffect` を 1 つ追加 (依存配列: `[positions]`):

```ts
useEffect(() => {
  const chart = chartRef.current
  if (!chart) return
  const pane = chart.panes()[0]
  if (!pane) return
  const series = candleSeriesRef.current
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

`tradeLinesVisible` トグルとは独立 (建値線にはトグルを設けない設計)。

### Cleanup

既存の chart unmount 時の cleanup (`bbFillRefs.current = {}` などの並び) に `positionPriceLineRef.current = null` を追加する。

## エッジケース

| ケース | 期待動作 |
|---|---|
| `positions === undefined` (初回ロード中) | primitive を attach しない。何も描画しない |
| `positions === []` (保有ポジションなし) | 既存 primitive があれば detach。線は消える |
| ポジション決済 → OPEN→CLOSED 遷移 | `usePositions` のポーリング (30s) で `positions` 配列が更新され、effect が走り直して該当線が消える |
| `price` が現在の価格軸表示範囲外 (ズームアウト/縮小時) | `priceToCoordinate` が `null` を返すので、その線だけ skip して他は描画継続 |
| symbol 切り替え | 親で `usePositions(symbolId)` が再 fetch → 新しい `positions` が effect に流れて再描画 |
| ボットが新規発注をしばらく止めている (`/trades` が `[]`) | 既存の縦線は表示されない (PR #268 仕様のまま) が、建値水平線は通常通り表示される |

## テスト戦略

`CandlestickChart` 自体は canvas プリミティブ描画 + lightweight-charts の wiring が中心で、既存にもユニットテストはなく E2E もない。本変更も同方針で、**手動検証**で受け入れ確認する。

### 手動検証チェックリスト

1. 現在保有中の BUY ポジション (LTC @ 8891.9, 0.9) に対して、緑の dashed 水平線が 8891.9 の高さに表示される
2. 右端に `8891.9 BUY 0.9` のラベルが線と同色 (緑) で表示される
3. ローソク本体・既存の MA / BB / Ichimoku 系列を線が邪魔しない (透過度 0.85)
4. `tradeLinesVisible` トグルを OFF にしても建値線は残る (縦線とは独立)
5. ズームアウトしたとき、価格軸範囲外のポジションがあっても他の描画は崩れない (= ポジション 1 本でも `priceToCoordinate=null` で例外を投げない)
6. ポジションを画面から決済し、30s 以内に水平線が消える
7. symbol セレクタで BTC など他銘柄に切り替えると、LTC 時の線は消えて、新銘柄に保有中ポジションがあればその建値線が出る

## 実装範囲外 / フォローアップ

- `useTradeHistory` の根本対策 (楽天 API ではなく `decision_log` または `trade_history` テーブルから配信する案) は本タスクと独立に進める
- TP/SL / トレーリングストップ価格の水平線描画は別タスク
