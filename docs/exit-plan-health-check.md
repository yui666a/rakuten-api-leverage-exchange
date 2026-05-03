# ExitPlan Phase 1 シャドウ運用ヘルスチェック

> Phase 1: ShadowHandler は約定イベントを listen して `exit_plans` テーブルに
> 書き込むだけ。発注パスへは干渉しない。本ドキュメントの SQL を 1 日 1 回程度
> 流して、楽天 API 建玉と DB ExitPlan の整合性が取れているか観察する。

設計書: `docs/superpowers/specs/2026-05-04-exit-plan-first-class-design.md`

## 1. open ExitPlan の一覧

```sql
SELECT id, position_id, symbol_id, side, entry_price,
       sl_percent, sl_atr_multiplier, tp_percent,
       trailing_mode, trailing_atr_multiplier,
       trailing_activated, trailing_hwm,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS created
FROM exit_plans
WHERE closed_at IS NULL
ORDER BY created_at DESC;
```

楽天サイトで現在保有している建玉数と件数が一致するか。

## 2. 直近 24h の close 履歴

```sql
SELECT id, position_id, symbol_id, side, entry_price,
       trailing_activated, trailing_hwm,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS opened,
       datetime(closed_at/1000,  'unixepoch', 'localtime') AS closed
FROM exit_plans
WHERE closed_at IS NOT NULL
  AND closed_at > (strftime('%s', 'now') - 86400) * 1000
ORDER BY closed_at DESC;
```

## 3. 同 position_id で複数 plan が作られていないか（UNIQUE 違反検知）

```sql
SELECT position_id, COUNT(*) AS n
FROM exit_plans
GROUP BY position_id
HAVING n > 1;
```

DB 制約で 1:1 を強制しているので空が期待値。

## 4. 楽天 API 建玉に対応する ExitPlan が存在しない孤児

bot ログで `shadow ExitPlan not found on close (orphan close)` を grep:

```bash
docker compose logs backend --since 24h | grep "orphan close" | wc -l
```

シャドウ運用初期は **bot 起動前から保有していた建玉** に対して plan が無いまま
close されるとここがカウントされる。完全に 0 にはならない（既存建玉ぶん）。
新規約定 → close のサイクルに対しては 0 が期待値。

## 5. 一定時間経った open plan が closed されているか（漏れ検知）

```sql
-- 24h 以上 open のままの plan
SELECT id, position_id, symbol_id, side, entry_price,
       datetime(created_at/1000, 'unixepoch', 'localtime') AS opened,
       (strftime('%s', 'now') - created_at/1000) / 3600 AS hours_open
FROM exit_plans
WHERE closed_at IS NULL
  AND created_at < (strftime('%s', 'now') - 86400) * 1000
ORDER BY created_at;
```

長時間 open は楽天 API 上では既に close されている可能性。Phase 2 の
Reconciler が無いので Phase 1 では手動確認。

## 6. ShadowHandler の永続化失敗ログ

```bash
docker compose logs backend --since 24h | grep -E "shadow ExitPlan (persist|find|close persist) failed"
```

シャドウなのでトレード継続には影響しないが、頻発する場合は DB アクセスや
プロファイル不整合の調査が必要。期待値は 0 件。

## 7. Phase 2 へ進む判断基準

Phase 2（既存 RiskManager の SL/TP/Trailing を Exit レイヤに移管）に進むには
以下を 1 週間継続して満たすこと:

- 新規約定 → close のサイクルで「孤児 close」「持続的に open のまま漏れ」が無い
- UNIQUE 違反が起きていない
- 永続化失敗ログが 0 件
- 楽天サイトの建玉一覧と `exit_plans WHERE closed_at IS NULL` の symbol_id / side / 件数が完全に一致
