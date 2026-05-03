# 直近の判断カード — TanStack Virtual 化設計

- 日付: 2026-05-03
- 対象: `frontend/src/components/RecentDecisionsCard.tsx`
- ステータス: Approved (brainstorm 段階の合意済)

## 背景

ダッシュボード（`/?symbol=LTC_JPY`）の「直近の判断」エリアは現在 `RECENT_LIMIT = 10` で固定取得し、すべての行を `<table>` に直接レンダーしている。
そのため、新しい判断が記録されると古い判断が表示から落ち、ダッシュボード上で過去の判断履歴を遡って確認できないという不満があった。

「全件を見る →」リンクから `/history?tab=decisions` ページへ遷移すれば閲覧できるが、ダッシュボードで完結したいという要望。

## ゴール

- ダッシュボード上で **直近 1 日分** の判断履歴を遡って閲覧できる。
- カード自体の専有面積は増やさない（高さは抑える）。
- 仮想スクロール（TanStack Virtual）で行の DOM 数を抑え、リスト件数が増えても表示パフォーマンスを保つ。

## 非ゴール

- 無限スクロール／ページング（API 側の追加実装が必要なため、本対応では行わない）。
- 期間切替 UI の追加（要件として提示されていない）。
- バックエンド `/decisions` API の変更（既に `limit` 最大 1000 まで対応済）。
- `/history` ページ側の改修。

## 要件

### 機能要件

1. ダッシュボードの「直近の判断」カードで、直近 1 日分相当の判断履歴を遡って閲覧できる。
2. カードの判断履歴テーブルは **12 行分** の高さに固定し、それ以上は縦スクロールで閲覧する。
3. テーブルのヘッダ行（時刻 / スタンス / 判断 / シグナル / 信頼度 / 結果 / 数量・価格 / 理由）はスクロール中も常時可視（sticky）にする。
4. 行の色分け（約定 / 却下 / HOLD など）、列の表示内容、`translateReason` などの既存表示ロジックは現状を踏襲する。
5. 既存の「全件を見る →」リンクは残し、より長期の履歴は引き続き `/history` で確認できる動線を維持する。

### 非機能要件

- 表示行数が増えても初期描画コストを抑える（DOM 上は visible + overscan のみ）。
- 既存テスト（`pnpm test`）が green のまま。
- 既存のカラーリング・余白・タイポグラフィは見た目の差分を最小化する。

## 設計

### データ取得

- `useDecisionLog(symbolId, limit)` フック自体は変更しない。
- 呼び出し側 `RecentDecisionsCard` の定数を変更する:
  - `RECENT_LIMIT: 10 → 200`
  - 根拠: PT15M（15 分足）= 1 日 96 本。BAR_CLOSE 以外のトリガ（tick 発火など）も `decision_log` に書き込まれるため、200 件を上限とすれば 1 日分を概ねカバーできる。
- `refetchInterval: 15_000` は据え置き。

### コンポーネント構造

`RecentDecisionsCard.tsx` の `MiniDecisionTable` を仮想スクロール対応に書き換える。
他のセクション（ヘッダ・スタンス表示・最終評価・「全件を見る →」リンク・reasoning ラベル）は現状維持。

```tsx
const ROW_HEIGHT = 36 // px。既存行の実測に合わせて確定する。
const VISIBLE_ROWS = 12

function VirtualizedDecisionTable({ decisions }: { decisions: DecisionLogItem[] }) {
  const parentRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: decisions.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 5,
  })

  return (
    <div
      ref={parentRef}
      className="overflow-auto rounded-2xl border border-white/8"
      style={{ height: VISIBLE_ROWS * ROW_HEIGHT }}
    >
      <table className="w-full text-xs" style={{ tableLayout: 'fixed' }}>
        <colgroup>{/* 列幅を固定 */}</colgroup>
        <thead className="sticky top-0 z-10 bg-white/5 ...">
          <tr>{/* 既存ヘッダ */}</tr>
        </thead>
        <tbody style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
          {virtualizer.getVirtualItems().map((vrow) => {
            const item = decisions[vrow.index]
            return (
              <tr
                key={item.id}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  height: ROW_HEIGHT,
                  transform: `translateY(${vrow.start}px)`,
                }}
                className={`...既存色分け...`}
              >
                {/* 既存セル */}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
```

- `position: absolute` で `<tr>` を浮かせるため、`<table>` には `table-layout: fixed` と `<colgroup>` で列幅を固定する必要がある。
- `<thead>` は `position: sticky; top: 0` で固定。背景色を不透明にしてスクロール時にコンテンツが透けないようにする。
- 行の色分けは現行 `rowBackground(item)` の戻り値クラスをそのまま `<tr>` に適用する。

### ライブラリ

- `@tanstack/react-virtual` v3.13+（`package.json` 導入済み）。
- 追加依存はない。

### 既存表示ロジックの再利用

以下は変更せず、そのまま `VirtualizedDecisionTable` 内のセル描画でも利用する:

- `INTENT_SHORT_LABEL`
- `translateReason`
- `outcomeLabel`
- `rowBackground`
- 時刻フォーマット（`toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })`）

### バックエンド

- 変更なし。`/decisions?symbolId=&limit=200` は既存ハンドラがそのまま処理する（既定 200・上限 1000）。

## 影響範囲

- 変更ファイル: `frontend/src/components/RecentDecisionsCard.tsx` のみ
- 依存追加: なし
- API 変更: なし
- 既存利用箇所: `routes/index.tsx` から `<RecentDecisionsCard />` で参照（インターフェース変更なし）

## テスト方針

- `cd frontend && pnpm test` で既存テストが green のまま。
- 動作確認手順:
  1. `docker compose up --build -d` 後、`http://localhost:33000/?symbol=LTC_JPY` を開く。
  2. 「直近の判断」カードのテーブルが 12 行ぶんの高さで表示されることを確認。
  3. テーブル内を縦スクロールし、過去の判断履歴（最大 200 件まで）が遡れることを確認。
  4. スクロール中もヘッダ行（時刻 / スタンス / …）が常時可視であることを確認。
  5. 行の色分け・列レイアウトが従来と同等であることを確認。
  6. 15 秒経過で再取得されたとき、リストが更新されることを確認。

## リスクと緩和

- **リスク**: `<table>` 内で `<tr>` を `position: absolute` にすると、ブラウザ既定の table レイアウトが効かず列幅が崩れる可能性。
  - **緩和**: `table-layout: fixed` ＋ `<colgroup>` で列幅を明示。各 `<th>` の `width` を設定する。
- **リスク**: 行高さを固定値（36px）にすることで、長文の「理由」セルが切り詰められる。
  - **緩和**: 既存実装も `max-w-[18rem] truncate` で切り詰めており、`title={rawReason}` でホバー時に全文表示しているため、現状踏襲とする。
- **リスク**: 案 A（`<table>` 維持）でレイアウトが破綻した場合、案 B（`div + grid` で組み直し）への切替が発生する可能性がある。
  - **緩和**: 実装中に動作確認しながら進める。破綻したら案 B に切り替える判断を許容。

## 参考

- 既存仕様
  - `frontend/src/components/RecentDecisionsCard.tsx`
  - `frontend/src/hooks/useDecisionLog.ts`
  - `backend/internal/interfaces/api/handler/decision.go`（`limit` 上限 1000）
- TanStack Virtual ドキュメント: `@tanstack/react-virtual` v3
