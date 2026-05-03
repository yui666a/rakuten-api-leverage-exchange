# RecentDecisionsCard 仮想スクロール化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ダッシュボードの「直近の判断」カードで直近 1 日分（最大 200 件）の判断履歴を、12 行ぶんの高さに固定したテーブル内で TanStack Virtual で遡って閲覧できるようにする。

**Architecture:** `RecentDecisionsCard.tsx` 1 ファイルに閉じた変更。`useDecisionLog` の取得件数を `200` に増やし、`MiniDecisionTable` を `useVirtualizer` ベースの `VirtualizedDecisionTable` に書き換える。`<table>` 構造を維持し、`<tbody>` 内で `<tr>` を `position: absolute` で浮かせて仮想化する（案A）。`<thead>` は sticky で常時可視。

**Tech Stack:**
- `@tanstack/react-virtual` v3.13+（`frontend/package.json` 導入済）
- React 19 / TypeScript
- Vitest + @testing-library/react

**Spec:** `docs/superpowers/specs/2026-05-03-recent-decisions-virtualization-design.md`

---

## File Structure

| Path | 役割 | 変更種別 |
|---|---|---|
| `frontend/src/components/RecentDecisionsCard.tsx` | 直近の判断カード本体。`MiniDecisionTable` を `VirtualizedDecisionTable` に置き換え、`RECENT_LIMIT` を 200 に変更 | Modify |
| `frontend/src/components/__tests__/RecentDecisionsCard.test.tsx` | 仮想化挙動と件数増加の回帰テスト | Create |

---

## Task 1: 仮想化テーブルのスモークテスト（赤）

**Files:**
- Create: `frontend/src/components/__tests__/RecentDecisionsCard.test.tsx`
- Refer: `frontend/src/hooks/__tests__/usePositions.test.tsx`（`createWrapper` パターン参照）

仮想化導入後の挙動を担保する最低限のテストを書く。fetch をモックして 200 件分のデータを返し、コンテナ内に `<thead>` の文言が描画されることを確認する。

- [ ] **Step 1: テストファイルを作成して失敗するテストを書く**

```tsx
// frontend/src/components/__tests__/RecentDecisionsCard.test.tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, createRouter, createRootRoute, RouterProvider } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { RecentDecisionsCard } from '../RecentDecisionsCard'
import type { DecisionLogItem, DecisionLogResponse } from '../../lib/api'

function makeItem(i: number): DecisionLogItem {
  return {
    id: i,
    barCloseAt: 1_700_000_000_000 + i * 60_000,
    sequenceInBar: 0,
    triggerKind: 'BAR_CLOSE',
    symbolId: 3,
    currencyPair: 'LTC_JPY',
    primaryInterval: 'PT15M',
    stance: 'TREND_FOLLOW',
    lastPrice: 10000 + i,
    signal: { action: 'HOLD', confidence: 0, reason: '' },
    risk: { outcome: 'SKIPPED', reason: '' },
    bookGate: { outcome: 'SKIPPED', reason: '' },
    order: { outcome: 'NOOP', orderId: 0, amount: 0, price: 0, error: '' },
    closedPositionId: 0,
    openedPositionId: 0,
    indicators: {},
    higherTfIndicators: {},
    createdAt: 1_700_000_000_000 + i * 60_000,
  }
}

function renderWithProviders(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({ component: () => <>{ui}</> })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('RecentDecisionsCard', () => {
  it('200 件取得しても thead が一度だけ描画され、行は仮想化される', async () => {
    const decisions = Array.from({ length: 200 }, (_, i) => makeItem(i))
    const response: DecisionLogResponse = { decisions, nextCursor: 0, hasMore: false }
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve(response),
    } as Response)

    renderWithProviders(
      <RecentDecisionsCard symbolId={3} strategy={undefined} rootSearch={{}} />,
    )

    await waitFor(() => {
      expect(screen.getByText('時刻')).toBeInTheDocument()
    })

    // 仮想化されているなら、200 件すべての <tr> は描画されない
    const rows = document.querySelectorAll('tbody tr')
    expect(rows.length).toBeLessThan(200)
    expect(rows.length).toBeGreaterThan(0)
  })

  it('limit=200 で /decisions を叩く', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ decisions: [], nextCursor: 0, hasMore: false }),
    } as Response)

    renderWithProviders(
      <RecentDecisionsCard symbolId={3} strategy={undefined} rootSearch={{}} />,
    )

    await waitFor(() => {
      expect(vi.mocked(fetch)).toHaveBeenCalled()
    })
    const url = vi.mocked(fetch).mock.calls[0][0] as string
    expect(url).toContain('limit=200')
  })
})
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `cd frontend && pnpm test -- RecentDecisionsCard`

Expected:
- 1 つ目のテスト: `rows.length` が 200（現状は仮想化されていないため全件 DOM に出る）→ FAIL
- 2 つ目のテスト: `limit=10` で叩いているため `expect(url).toContain('limit=200')` が FAIL

赤を確認したら次へ。

- [ ] **Step 3: コミット（赤テスト）**

```bash
git add frontend/src/components/__tests__/RecentDecisionsCard.test.tsx
git commit -m "test(ui): RecentDecisionsCard 仮想化と取得件数の赤テスト"
```

---

## Task 2: 取得件数を 200 に変更

**Files:**
- Modify: `frontend/src/components/RecentDecisionsCard.tsx:7`

- [ ] **Step 1: `RECENT_LIMIT` を 10 → 200 に変更**

`frontend/src/components/RecentDecisionsCard.tsx` の 7 行目を以下のように変更:

```tsx
const RECENT_LIMIT = 200
```

- [ ] **Step 2: 2 つ目のテストが PASS することを確認**

Run: `cd frontend && pnpm test -- RecentDecisionsCard -t "limit=200"`

Expected: PASS

1 つ目（仮想化テスト）はまだ FAIL のまま。

- [ ] **Step 3: コミット**

```bash
git add frontend/src/components/RecentDecisionsCard.tsx
git commit -m "feat(ui): RecentDecisionsCard の取得件数を 10 → 200 に拡張"
```

---

## Task 3: VirtualizedDecisionTable を実装

**Files:**
- Modify: `frontend/src/components/RecentDecisionsCard.tsx`

`MiniDecisionTable` を `VirtualizedDecisionTable` に置き換える。`<table>` 構造は維持し、`<tbody>` 内で `<tr>` を `position: absolute` で浮かせて仮想化する。`<thead>` は sticky。

- [ ] **Step 1: import を追加**

ファイル先頭の import を以下のように変更:

```tsx
import { useRef } from 'react'
import { Link } from '@tanstack/react-router'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { DecisionLogItem, StrategyResponse } from '../lib/api'
import { useDecisionLog } from '../hooks/useDecisionLog'
import { translateReason } from '../lib/decisionReasonI18n'
import { StanceLegendPopover } from './StanceLegendPopover'
```

- [ ] **Step 2: 定数を追加**

`const RECENT_LIMIT = 200` の直下に以下を追加:

```tsx
const ROW_HEIGHT = 36 // px。<tr> 高さ固定（py-2 + 12px line-height ベース）。
const VISIBLE_ROWS = 12
```

- [ ] **Step 3: `MiniDecisionTable` を `VirtualizedDecisionTable` に置き換え**

`MiniDecisionTable` 関数（70〜94 行目）を以下に丸ごと置き換える:

```tsx
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
        <colgroup>
          <col style={{ width: '4.5rem' }} />
          <col style={{ width: '7rem' }} />
          <col style={{ width: '5rem' }} />
          <col style={{ width: '4rem' }} />
          <col style={{ width: '4.5rem' }} />
          <col style={{ width: '6rem' }} />
          <col style={{ width: '8rem' }} />
          <col />
        </colgroup>
        <thead className="sticky top-0 z-10 bg-bg-card text-[0.65rem] uppercase tracking-[0.18em] text-text-secondary">
          <tr>
            <th className="px-3 py-2 text-left">時刻</th>
            <th className="px-3 py-2 text-left">スタンス</th>
            <th className="px-3 py-2 text-left">判断</th>
            <th className="px-3 py-2 text-left">シグナル</th>
            <th className="px-3 py-2 text-right">信頼度</th>
            <th className="px-3 py-2 text-left">結果</th>
            <th className="px-3 py-2 text-right">数量/価格</th>
            <th className="px-3 py-2 text-left">理由</th>
          </tr>
        </thead>
        <tbody style={{ height: virtualizer.getTotalSize(), position: 'relative', display: 'block' }}>
          {virtualizer.getVirtualItems().map((vrow) => {
            const item = decisions[vrow.index]
            return (
              <VirtualRow
                key={item.id}
                item={item}
                top={vrow.start}
                height={ROW_HEIGHT}
              />
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function VirtualRow({
  item,
  top,
  height,
}: {
  item: DecisionLogItem
  top: number
  height: number
}) {
  const bg = rowBackground(item)
  const rawReason =
    item.decision?.reason ||
    item.signal.reason ||
    item.risk.reason ||
    item.bookGate.reason ||
    item.order.error ||
    '—'
  const reason = translateReason(rawReason)
  const outcome = outcomeLabel(item)
  const intent = item.decision?.intent ?? ''
  return (
    <tr
      className={`border-t border-white/8 ${bg}`}
      style={{
        position: 'absolute',
        top: 0,
        left: 0,
        width: '100%',
        height,
        transform: `translateY(${top}px)`,
        display: 'table',
        tableLayout: 'fixed',
      }}
    >
      <td className="px-3 py-2 whitespace-nowrap" style={{ width: '4.5rem' }}>
        {new Date(item.barCloseAt).toLocaleTimeString('ja-JP', {
          hour: '2-digit',
          minute: '2-digit',
        })}
      </td>
      <td className="px-3 py-2" style={{ width: '7rem' }}>{item.stance || '—'}</td>
      <td className="px-3 py-2 whitespace-nowrap" style={{ width: '5rem' }}>
        {INTENT_SHORT_LABEL[intent]}
      </td>
      <td className="px-3 py-2 font-medium" style={{ width: '4rem' }}>
        {item.signal.action}
      </td>
      <td className="px-3 py-2 text-right" style={{ width: '4.5rem' }}>
        {item.signal.action === 'HOLD'
          ? '—'
          : `${(item.signal.confidence * 100).toFixed(1)}%`}
      </td>
      <td className="px-3 py-2 whitespace-nowrap" style={{ width: '6rem' }}>
        {outcome}
      </td>
      <td
        className="px-3 py-2 text-right whitespace-nowrap"
        style={{ width: '8rem' }}
      >
        {item.order.outcome === 'NOOP'
          ? '—'
          : `${item.order.amount} @ ${item.order.price.toLocaleString('ja-JP')}`}
      </td>
      <td className="truncate px-3 py-2 text-text-secondary" title={rawReason}>
        {reason}
      </td>
    </tr>
  )
}
```

注: `<tbody>` を `display: block`、`<tr>` を `display: table; table-layout: fixed; width: 100%` にしているのは、`<tr>` を `position: absolute` で浮かせると `<table>` 既定のレイアウトが効かず列幅が崩れるための緩和策。`<colgroup>` だけでは絶対配置の `<tr>` には適用されないため、各 `<td>` にも `style={{ width: ... }}` を二重指定している。

- [ ] **Step 4: 旧 `MiniRow` 関数を削除**

`MiniRow` 関数（106〜145 行目あたり）は `VirtualRow` に置き換わったため削除する。`INTENT_SHORT_LABEL` / `pickReasoningLabel` / `stanceColorClass` / `rowBackground` / `outcomeLabel` は残す。

- [ ] **Step 5: 呼び出し側を差し替え**

`RecentDecisionsCard` 関数内 63 行目の `<MiniDecisionTable decisions={decisions} />` を以下に変更:

```tsx
<VirtualizedDecisionTable decisions={decisions} />
```

- [ ] **Step 6: テストを実行して両方 PASS することを確認**

Run: `cd frontend && pnpm test -- RecentDecisionsCard`

Expected: 2 つとも PASS

注: `useVirtualizer` は jsdom 環境で `getBoundingClientRect` がゼロを返すため virtualItems が空配列になる可能性がある。その場合は最初のテストの `expect(rows.length).toBeGreaterThan(0)` が FAIL する。FAIL したら、テスト側でそのアサーションを「`rows.length === 0` 許容」に緩める or `Element.prototype.getBoundingClientRect` をモックする。Step 6 で FAIL した場合の対処:

```tsx
// テストファイルの beforeEach に追加
beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn())
  // jsdom は要素サイズを 0 にするため、useVirtualizer 用にモック
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', {
    configurable: true,
    value: VISIBLE_ROWS * ROW_HEIGHT, // 432
  })
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', {
    configurable: true,
    value: 800,
  })
})
```

ただし `VISIBLE_ROWS` / `ROW_HEIGHT` はテストファイルから import できないので、ハードコードで `432` / `800` と書く。

- [ ] **Step 7: 型チェックを通す**

Run: `cd frontend && pnpm exec tsc --noEmit`

Expected: エラーなし

- [ ] **Step 8: コミット**

```bash
git add frontend/src/components/RecentDecisionsCard.tsx frontend/src/components/__tests__/RecentDecisionsCard.test.tsx
git commit -m "feat(ui): RecentDecisionsCard を TanStack Virtual で仮想スクロール化"
```

---

## Task 4: 手動動作確認

**Files:** なし（ブラウザでの目視確認）

- [ ] **Step 1: Docker Compose を再ビルドして起動**

Run: `docker compose up --build -d`

frontend は bind mount なので HMR で反映される想定だが、念のため再ビルド。

- [ ] **Step 2: ブラウザで確認**

`http://localhost:33000/?symbol=LTC_JPY` を開き、以下を目視確認:

1. 「直近の判断」カードのテーブルが **12 行ぶんの高さ（432px 前後）** で表示される。
2. テーブル内を縦スクロールすると、過去の判断履歴を遡れる。
3. スクロール中もヘッダ行（時刻 / スタンス / 判断 / シグナル / 信頼度 / 結果 / 数量・価格 / 理由）が常時可視。
4. 行の色分け（約定=緑 / 却下=赤 / HOLD=黄など）が従来通り機能している。
5. 列幅が崩れていない。
6. しばらく放置（15 秒以上）して再取得が走り、リストが更新されることを確認。

- [ ] **Step 3: 失敗時の判断**

列幅が崩れる、行が重なる、スクロールが効かない等の崩壊が起きたら、案 B（`<table>` をやめて `div + grid` で組み直す）への切り替えを検討する。その判断は実装者が動作を見て決める。

- [ ] **Step 4: PR 用の最終チェック**

Run: `cd frontend && pnpm test`

Expected: 全 green

Run: `cd frontend && pnpm exec tsc --noEmit`

Expected: エラーなし

---

## Self-Review

**Spec coverage:**

| Spec の要件 | 実装タスク |
|---|---|
| 直近 1 日分（200 件）取得 | Task 2 |
| 12 行ぶんの高さに固定 | Task 3（`VISIBLE_ROWS = 12`） |
| sticky thead | Task 3（`<thead className="sticky top-0 ...">`） |
| 既存色分け・列内容を踏襲 | Task 3（`rowBackground` / `INTENT_SHORT_LABEL` / `outcomeLabel` / `translateReason` を流用） |
| 「全件を見る →」維持 | 既存ヘッダのまま（変更なし） |
| `useDecisionLog` 不変 | Task 2（呼び出し側のみ変更） |
| バックエンド変更なし | 該当なし（コード変更なし） |
| `pnpm test` green | Task 1 + Task 3 + Task 4 |
| 案A破綻時のフォールバック判断 | Task 4 Step 3 |

**Placeholder scan:** TBD/TODO/「適切に」「同様に」等は不在。すべての code step に実コードあり。

**Type consistency:** `VirtualizedDecisionTable` / `VirtualRow` / `ROW_HEIGHT` / `VISIBLE_ROWS` / `RECENT_LIMIT` の名前を全タスクで一致させた。`MiniDecisionTable` / `MiniRow` は Task 3 で完全に削除する旨を明記。
