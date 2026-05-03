# Decision Log 健全性チェック

> 15 分足クローズごとに `decision_log` (ライブ) と `backtest_decision_log` (バックテスト) が
> 正しく書き込まれているかを SQL で 1 発確認する手順。
>
> 仕様: `docs/superpowers/specs/2026-04-26-decision-log-design.md`
> 関連 PR: #197 〜 #204

## いつ叩くか

- decision-log 機能をデプロイ / 再起動した直後 (= 1 回でも 15 分足クローズが回ったあと)
- 設定変更や戦略プロファイル切替後、しばらく動かしてから
- 「最近シグナル鳴ってない気がする」と感じたとき (本当に HOLD ばかりなのか、recorder が壊れてるだけなのかを切り分け)

## 何が分かるか

| 指標 | 解釈 |
|---|---|
| 24h の総行数 | 0 件 = 記録ゼロ。recorder が起動していないか pipeline が止まっている |
| `signal_action` 内訳 | BUY/SELL が 1 件もない = Strategy が機能していない疑い (HOLD 100%) |
| `risk_outcome` 内訳 + 理由 | REJECTED が圧倒的、しかも理由が 1 つに偏る = 設定ミス / daily loss 上限張り付き |
| `book_gate_outcome` 内訳 | VETOED 多発 = pre-trade book gate が厳しすぎる |
| `order_outcome` 内訳 | FAILED 多発 = 楽天 API エラー / 残高不足 |
| `trigger_kind` 内訳 | TICK_SLTP / TICK_TRAILING が 0 = TickRiskHandler の wiring に問題 |
| `indicators_json` が空 | recorder が IndicatorEvent を受信できていない |
| 親 BAR_CLOSE のない tick 行 | 同一バー内の sequence 振りが壊れている |

## 実行コマンド

`backend` コンテナが起動している前提。

```bash
docker compose exec backend sqlite3 /app/backend/db/trading.db <<'SQL'
.headers on
.mode column

SELECT '=== 24h totals ===' AS section;
SELECT COUNT(*) AS rows_24h
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000;

SELECT '=== by signal_action ===' AS section;
SELECT signal_action, COUNT(*) AS n
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
GROUP BY signal_action ORDER BY n DESC;

SELECT '=== by risk_outcome (with reasons) ===' AS section;
SELECT risk_outcome, COUNT(*) AS n,
       GROUP_CONCAT(DISTINCT NULLIF(risk_reason,'')) AS reasons
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
GROUP BY risk_outcome;

SELECT '=== by book_gate_outcome ===' AS section;
SELECT book_gate_outcome, COUNT(*) AS n,
       GROUP_CONCAT(DISTINCT NULLIF(book_gate_reason,'')) AS reasons
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
GROUP BY book_gate_outcome;

SELECT '=== by order_outcome ===' AS section;
SELECT order_outcome, COUNT(*) AS n
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
GROUP BY order_outcome;

SELECT '=== by trigger_kind ===' AS section;
SELECT trigger_kind, COUNT(*) AS n
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
GROUP BY trigger_kind;

SELECT '=== anomaly: empty indicators ===' AS section;
SELECT COUNT(*) AS empty_indicators
FROM decision_log
WHERE bar_close_at >= (strftime('%s','now') - 86400) * 1000
  AND (indicators_json = '' OR indicators_json = '{}');

SELECT '=== anomaly: tick rows without parent BAR_CLOSE in same 15m bar ===' AS section;
SELECT COUNT(*) AS orphan_ticks
FROM decision_log t
WHERE t.trigger_kind IN ('TICK_SLTP','TICK_TRAILING')
  AND t.bar_close_at >= (strftime('%s','now') - 86400) * 1000
  AND NOT EXISTS (
    SELECT 1 FROM decision_log b
    WHERE b.symbol_id = t.symbol_id
      AND b.trigger_kind = 'BAR_CLOSE'
      AND b.bar_close_at <= t.bar_close_at
      AND b.bar_close_at >= t.bar_close_at - 900000
  );
SQL
```

## 期待されるおおよその出力 (運用 24h、BUY/SELL が数回出た場合)

```
=== 24h totals ===
rows_24h
96            -- PT15M なら 24h × 4 = 96 行が基準

=== by signal_action ===
signal_action  n
HOLD           90
BUY            4
SELL           2

=== by risk_outcome ===
risk_outcome  n   reasons
APPROVED      6
SKIPPED       90              -- HOLD 行は SKIPPED で正しい

=== by trigger_kind ===
trigger_kind   n
BAR_CLOSE      96
TICK_SLTP      2              -- SL/TP 発動 2 回
TICK_TRAILING  0

=== anomaly: empty indicators ===
empty_indicators
0

=== anomaly: tick rows without parent BAR_CLOSE in same 15m bar ===
orphan_ticks
0
```

## 異常パターンと対処

| 観測 | 疑い | 確認 |
|---|---|---|
| `rows_24h = 0` | recorder が登録されていない | `docker compose logs backend \| grep "decision recorder attached"` で起動ログ確認 |
| `signal_action` が HOLD 100% | Strategy または閾値設定の問題 | `risk_reason` を見る。`adxBlock` 系なら ADX 閾値、`MTF filter` なら上位足設定を見直し |
| `risk_outcome=REJECTED` 大半が同じ理由 | 設定上限張り付き | `daily loss limit exceeded` なら `MAX_DAILY_LOSS` 確認、`cooldown` なら連続損切り後の保護期間 |
| `book_gate_outcome=VETOED` 多発 | `MAX_SLIPPAGE_BPS` / `MAX_BOOK_SIDE_PCT` が厳しすぎる | `book_gate_reason` (`thin_book_pre_trade` / `slippage_exceeds_threshold`) で内訳判別 |
| `order_outcome=FAILED` あり | 楽天 API エラー | `decision_log.order_error` カラムを直接 SELECT して中身を見る |
| `trigger_kind` に TICK_* が一切ない | tick 経路が壊れている、または単に SL/TP に当たってない | `WHERE trigger_kind IN ('TICK_SLTP','TICK_TRAILING')` で全期間を見る。それでも 0 ならコード側 |
| `empty_indicators > 0` | recorder の IndicatorEvent 受信に欠落 | 該当行を SELECT して `created_at` / `bar_close_at` を確認、近傍ログを照合 |
| `orphan_ticks > 0` | tick 行の sequence 計算がおかしい | バーまたぎ前後の挙動を疑う。`SELECT * FROM decision_log WHERE bar_close_at = <該当>` で前後比較 |

## Phase 1 三層分離カラムの確認 (2026-05-02 〜)

PR1 (#232) 〜 PR4 (#235) で `decision_log` / `backtest_decision_log` に 6 列追加:
`signal_direction`, `signal_strength`, `decision_intent`, `decision_side`, `decision_reason`, `exit_policy_outcome`.

PR2 以降の行はすべての列が埋まっているはず。空文字 (`= ''`) が混じる場合は recorder が新ルートを購読し損なっている疑いあり。

```sql
-- 直近 24h の Decision レイヤ出力サマリ。NEW_ENTRY/HOLD/EXIT_CANDIDATE/COOLDOWN_BLOCKED の比率を把握する。
SELECT
  signal_direction,
  decision_intent,
  decision_side,
  COUNT(*) AS rows
FROM decision_log
WHERE bar_close_at > strftime('%s','now','-24 hours') * 1000
GROUP BY signal_direction, decision_intent, decision_side
ORDER BY rows DESC;

-- PR2 以降の行で signal_direction が空のものは異常 (recorder が新ルートを取り損ねている可能性)
SELECT COUNT(*) AS empty_direction_rows
FROM decision_log
WHERE bar_close_at > strftime('%s','now','-24 hours') * 1000
  AND signal_direction = '';

-- EXIT_CANDIDATE が出ているか (両建て総額判定バグ修正の動作確認)
SELECT bar_close_at, signal_action, signal_direction, decision_intent, decision_side, decision_reason
FROM decision_log
WHERE decision_intent = 'EXIT_CANDIDATE'
ORDER BY bar_close_at DESC
LIMIT 20;

-- COOLDOWN_BLOCKED が出ているか (close 約定後 EntryCooldownSec 秒の抑制動作確認)
SELECT bar_close_at, decision_intent, decision_reason
FROM decision_log
WHERE decision_intent = 'COOLDOWN_BLOCKED'
ORDER BY bar_close_at DESC
LIMIT 20;
```

期待される観察:

- `signal_direction` 別: NEUTRAL が圧倒的、BULLISH / BEARISH が時々
- `decision_intent` 別: HOLD が大半、NEW_ENTRY が時々、EXIT_CANDIDATE は保有中の signal 反転時のみ、COOLDOWN_BLOCKED は close 直後 60 秒以内
- `signal_action` (旧) と `signal_direction` (新) の整合: BUY ↔ BULLISH、SELL ↔ BEARISH、HOLD ↔ NEUTRAL

## バックテスト側

`backtest_decision_log` も同じ要領で確認できる。違いは `backtest_run_id` で必ず scope すること:

```sql
-- 直近の run id 一覧
SELECT DISTINCT backtest_run_id, COUNT(*) AS n
FROM backtest_decision_log
GROUP BY backtest_run_id
ORDER BY MAX(created_at) DESC
LIMIT 10;

-- 特定 run の集計
SELECT signal_action, COUNT(*) AS n
FROM backtest_decision_log
WHERE backtest_run_id = '<RUN_ID>'
GROUP BY signal_action;
```

3 日経過した行は retention goroutine が自動削除する。即時削除は API 経由:

```bash
curl -X DELETE http://localhost:38080/api/v1/backtest/results/<RUN_ID>/decisions
```
