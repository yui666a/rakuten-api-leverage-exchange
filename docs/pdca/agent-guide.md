# PDCA Strategy Optimizer — エージェント向けガイド

このドキュメントは、別のセッションに入ってくる Claude Code エージェント（または人間の開発者）が、この機能の **全体像** と **実際の使い方** を 5 分で把握し、PDCA を自律的に回せるようになることを目的とする。

関連ドキュメント:

- [`docs/pdca/README.md`](./README.md) — 運用ガイド（Plan/Do/Check/Act の手順書）
- [`docs/pdca/_template.md`](./_template.md) — サイクル記録テンプレート
- [`docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md`](../superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md) — 設計の一次情報

---

## 1. この機能でできること

LTC/JPY 固定の自動売買戦略について、**バックテストを繰り返して戦略パラメータ・ロジックを最適化するための PDCA 基盤** が揃っている。

| できること | 具体的には |
|---|---|
| 戦略パラメータの外部設定化 | 戦略の閾値・条件は `backend/profiles/<name>.json` に JSON として宣言。コード変更不要でパラメータ実験ができる |
| 本番と実験の分離 | `production.json` は現行本番戦略。`experiment_*.json` を別途作って本番ロジックを壊さず試行できる |
| バックテストの再現可能な実行 | CLI・API の両方からプロファイル指定で実行可能。実行コマンドはそのままサイクル記録に転記できる |
| PDCA メタデータの永続化 | 各バックテスト結果に `profileName` / `pdcaCycleId` / `hypothesis` / `parentResultId` を保存。系譜を辿れる |
| 2 週間勝率の自動計測 | `BiweeklyWinRate`（2 週間スライドウィンドウ平均、0-100 スケール）が `BacktestSummary` に自動で含まれる |
| 一覧画面での系譜ナビ | フロントの「バックテスト一覧」でプロファイル・サイクル・親子関係フィルタ + 親 ID クリック絞り込み |
| 新指標追加の拡張ポイント | `infrastructure/indicator/` に指標を追加し、プロファイルの `signal_rules` から参照する経路が整っている（Level 3 エスカレーション） |

### できないこと（意図的に範囲外）

- フロント上での「本番昇格」ボタン — 手動で JSON をコピーする運用
- PDCA サイクルの自動 cron 実行 — 必ずエージェント/人が手動で回す
- 複数通貨対応 — LTC/JPY 固定
- 指標の **計算期間** (RSI 14, SMA 20/50 等) をプロファイルから変更 — 現在の `IndicatorCalculator` はハードコード。プロファイル内の `indicators.*` は **メタデータ（記録用）** であって計算時には適用されない。期間を変えたい場合は `IndicatorCalculator` 側の改修が必要

---

## 2. システムの構成要素

### 主要ファイル / パッケージ

| レイヤ | パス | 役割 |
|---|---|---|
| Port | `backend/internal/domain/port/strategy.go` | `Strategy` インターフェース。`Evaluate(ctx, indicators, htfIndicators, lastPrice, now) (*Signal, error)` と `Name() string` |
| Entity | `backend/internal/domain/entity/strategy_config.go` | `StrategyProfile` およびネスト設定（`IndicatorConfig` / `StanceRulesConfig` / `SignalRulesConfig`（ADX/Stoch gate 含む）/ `HTFFilterConfig`（`mode: "ema"\|"ichimoku"`）/ `StrategyRiskConfig`（ATR multiplier 含む））+ `Validate()` |
| Entity | `backend/internal/domain/entity/indicator.go` | `IndicatorSet`（SMA/EMA/RSI/MACD/BB/ATR/Volume + **ADX/Stoch/Ichimoku**） |
| Entity | `backend/internal/domain/entity/backtest.go` / `walk_forward.go` / `aggregate_json.go` | `BacktestSummary`（PR-1 breakdown + PR-3 DD/TiM/Expectancy）/ `MultiPeriodAggregate`（NaN/±Inf JSON null round-trip）/ `WalkForwardPersisted`（envelope DB 型） |
| Infrastructure | `backend/internal/infrastructure/strategyprofile/` | `ResolveProfilePath(baseDir, name)` と `Loader.Load(name)`。パストラバーサル対策 + `DisallowUnknownFields` |
| Infrastructure | `backend/internal/infrastructure/indicator/` | `adx.go` / `stochastics.go` / `ichimoku.go` 他。FE の描画ロジックと golden value で整合 |
| Infrastructure | `backend/internal/infrastructure/backtest/` | `result_repository.go` / `multi_period_repository.go` / `walk_forward_repository.go` / `simulator.go` |
| UseCase | `backend/internal/usecase/strategy/` | `DefaultStrategy`（現行ロジックをラップ）/ `ConfigurableStrategy`（プロファイル駆動）/ `StrategyRegistry` + 配線確認テスト `adx_gate_test.go` / `stoch_gate_test.go` / `htf_ichimoku_test.go` |
| UseCase | `backend/internal/usecase/strategy.go` / `stance.go` | `StrategyEngineOptions`（ADX/Stoch gate + HTF mode）/ `RuleBasedStanceResolverOptions` で閾値注入可能。ゼロ値→デフォルト値補完 |
| UseCase | `backend/internal/usecase/backtest/biweekly.go` | `ComputeBiweeklyWinRate(trades, periodFrom, periodTo)` — 14日ウィンドウ/1日スライド、3件未満ペナルティ、カバレッジ 50% 未満で 0 |
| UseCase | `backend/internal/usecase/backtest/breakdown.go` / `drawdown_detail.go` | PR-1 Exit 理由別 / シグナル別サマリ + PR-3 DD 履歴 / Time-in-market / Expectancy |
| UseCase | `backend/internal/usecase/backtest/multi_period_runner.go` / `aggregate.go` | PR-2 複数期間並列実行 + RobustnessScore |
| UseCase | `backend/internal/usecase/backtest/walkforward.go` / `walkforward_runner.go` | PR-13 `ComputeWindows` / `ExpandGrid` / **`ApplyOverrides`（Stoch gate パス対応済）** / IS-best 選択 → OOS 検証 |
| Repository | `backend/internal/domain/repository/backtest_result.go` + `multi_period_result.go` + `walk_forward_result.go` + `infrastructure/backtest/*_repository.go` | PDCA フィルタ + envelope 永続化 + `ErrParentResultSelfReference` / `ErrParentResultNotFound` |
| API | `backend/internal/interfaces/api/handler/backtest.go` / `backtest_multi.go` / `backtest_walkforward.go` | `POST /backtest/run` / `run-multi` / `walk-forward` と各 GET。プロファイル解決 + PDCA メタデータ + 422 マッピング |
| CLI | `backend/cmd/backtest/main.go` + `walkforward.go` | `run` / `optimize` / `refine` / `download` / **`walk-forward`** サブコマンド。`--profile` + `flag.Visit` で明示指定を検出 |
| Profiles | `backend/profiles/production.json` | 現行本番ロジック（2026-04-21 v4b promotion） |
| Frontend | `frontend/src/routes/backtest.tsx` / `backtest-multi.tsx` / `walk-forward.tsx` + `src/hooks/useBacktest.ts` / `useMultiPeriod.ts` / `useWalkForward.ts` / `src/lib/api.ts` / `components/ADXChart.tsx` | PDCA 詳細・マルチ期間ランキング・WFO ページ + ADX パネル |

### DB スキーマ

`backtest_results` テーブルに以下のカラムが追加されている:

| カラム | 型 | デフォルト | 用途 |
|---|---|---|---|
| `profile_name` | TEXT NOT NULL | `''` | 使用プロファイル名 |
| `pdca_cycle_id` | TEXT NOT NULL | `''` | PDCA サイクル識別子 |
| `hypothesis` | TEXT NOT NULL | `''` | サイクル仮説 |
| `parent_result_id` | TEXT NULL (FK→self, SET NULL on delete) | `NULL` | 親結果 ID。NULL=ルート |
| `biweekly_win_rate` | REAL NOT NULL | `0` | 2 週間スライド勝率平均 (0-100) |

インデックスは `parent_result_id` / `profile_name` / `pdca_cycle_id` に部分インデックス（WHERE で NULL/empty を除外）。

---

## 3. 実際の使い方

### 3.1 CLI でのバックテスト実行

```bash
cd backend

# 本番プロファイルで実行
go run ./cmd/backtest run \
  --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv \
  --from 2025-01-01 --to 2025-01-31

# 個別フラグで上書き（--stop-loss を明示指定すると profile の値を無視）
go run ./cmd/backtest run \
  --profile production \
  --data data/candles_LTC_JPY_PT15M.csv \
  --stop-loss 3
```

- `--profile` は **ファイル名のみ**（拡張子なし）。`profiles/<name>.json` に解決される
- 不正な名前（スラッシュ・`..`・空文字）はエラー終了
- `flag.Visit` で明示指定された値のみがプロファイル値を上書きする（明示されなければプロファイル値を採用）

### 3.2 API でのバックテスト実行

```bash
# 本番プロファイル + PDCA メタデータ
curl -X POST http://localhost:38080/api/v1/backtest/run \
  -H 'Content-Type: application/json' \
  -d '{
    "data": "data/candles_LTC_JPY_PT15M.csv",
    "from": "2025-01-01",
    "to": "2025-01-31",
    "initialBalance": 100000,
    "tradeAmount": 0.1,
    "profileName": "production",
    "pdcaCycleId": "2026-04-17_cycle01",
    "hypothesis": "baseline measurement"
  }'
```

**レスポンスのポイント**:

```json
{
  "id": "01KPCG5WZ7PBNQTKB0D574QYVH",
  "profileName": "production",
  "pdcaCycleId": "2026-04-17_cycle01",
  "hypothesis": "baseline measurement",
  "parentResultId": null,
  "summary": {
    "totalReturn": -0.0447,
    "maxDrawdown": 0.0488,
    "biweeklyWinRate": 33.42,
    "...": "..."
  }
}
```

`id` は以降のサイクルで `parentResultId` として使える。

### 3.3 API の 422 ガード

```bash
# 存在しない親 ID → 422
curl -X POST http://localhost:38080/api/v1/backtest/run \
  -d '{..., "parentResultId": "does-not-exist"}'
# → {"error":"save backtest result: backtest_result: parent_result_id does not reference an existing row"}
# → HTTP 422
```

`ErrParentResultSelfReference` / `ErrParentResultNotFound` は **ドメインセンチネルエラー**。`errors.Is` で判別可能。ハンドラ層で 422 にマップ。

### 3.4 一覧 API のフィルタ

```bash
# プロファイルで絞り込み
curl 'http://localhost:38080/api/v1/backtest/results?profileName=production'

# PDCA サイクル ID で絞り込み
curl 'http://localhost:38080/api/v1/backtest/results?pdcaCycleId=2026-04-17_cycle01'

# 親を持つ結果のみ
curl 'http://localhost:38080/api/v1/backtest/results?hasParent=true'

# ルートのみ
curl 'http://localhost:38080/api/v1/backtest/results?hasParent=false'

# 特定親の子一覧（系譜ナビ）
curl 'http://localhost:38080/api/v1/backtest/results?parentResultId=01KPCG5W...'
```

**precedence**: `parentResultId` 指定時は `hasParent` を無視。`hasParent` の値は `true` / `false` のみ（他は 400）。

### 3.5 フロント画面

`http://localhost:33000/backtest` の「バックテスト一覧」セクション:

- **プロファイル** ドロップダウン — 現ページの distinct プロファイルから選択
- **親子関係** ドロップダウン — すべて / 親あり (PDCA継続) / 親なし (ルート)
- **PDCA / manual バッジ** — 各行の ID 列横
- **親 ID リンク** — クリックで `parentResultId` フィルタ適用、×ボタンで解除
- **PDCA Cycle / 親** カラム — 各行の系譜情報を表示

### 3.6 新しい実験プロファイルを作る

```bash
# 本番をコピーして実験ベースを作る
cp backend/profiles/production.json backend/profiles/experiment_2026-04-17_01.json

# JSON を編集（例: RSI の overbought を 75 → 70 に緩める）
# その後 CLI で実行
cd backend
go run ./cmd/backtest run \
  --profile experiment_2026-04-17_01 \
  --data data/candles_LTC_JPY_PT15M.csv \
  --data-htf data/candles_LTC_JPY_PT1H.csv
```

**プロファイル名の制約**: `^[a-zA-Z0-9_-]+$`。ドット・スラッシュ・スペース NG。

### 3.7 新指標を追加する（Level 3 エスカレーション）

1. `backend/internal/infrastructure/indicator/<name>.go` に計算ロジックと単体テストを追加
2. `backend/internal/domain/entity/indicator.go` の `IndicatorSet` にフィールドを追加
3. `backend/internal/usecase/indicator.go` で新指標を計算し `IndicatorSet` に詰める
4. `backend/internal/domain/entity/strategy_config.go` の `SignalRulesConfig` に対応する設定を追加（必要なら）
5. `backend/internal/usecase/strategy.go` の `StrategyEngineOptions` に新しい threshold を追加、`evaluate*` 関数で参照
6. `backend/internal/usecase/strategy/configurable_strategy.go` で profile → options のマッピングを追加
7. `backend/profiles/production.json` に既存挙動と等価な値を追加（`production == default` の不変条件を維持）
8. `TestConfigurableStrategy_EquivalentToDefault` が引き続き通ることを確認

---

## 4. PDCA を回すためのヒント

### 4.1 仮説の立て方

- **1 サイクル = 1 仮説**。複数パラメータを同時に動かすと何が効いたか分離できない
- 仮説は「なぜ改善するか」を明文化する。数字だけでなく市場挙動への因果を言語化
- 「とりあえず広げる / 狭める」系の探索は Optimizer (CLI `optimize` サブコマンド) で一気に掛ける

### 4.2 評価基準の優先順位

必ずこの順で判定する:

1. **必須制約**: `MaxDrawdown ≤ 20%` — 超えたら即 reject、他の数字がどれだけ良くても採用しない
2. **主目的**: `TotalReturn` の向上
3. **副目的**: `BiweeklyWinRate` の 80% 接近
4. **参考**: `SharpeRatio` / `ProfitFactor` / `WinRate`

### 4.3 BiweeklyWinRate の解釈

- 0-100 スケール。目標 80
- **値 0 には 2 意味**:
  - 本当に勝率 0%
  - カバレッジ（3 件以上のトレードがあるウィンドウ割合）< 50%（信頼不可）
- 低頻度戦略は前者で見かけの勝率が高くなるのを防ぐ設計
- 判定には必ず `TotalTrades` と合わせて見る

### 4.4 期間の選び方

- **短すぎる期間は BiweeklyWinRate が 0 になる**（14 日未満）
- 最低 1 ヶ月、推奨 3〜6 ヶ月
- 相場レジームが変わる期間（トレンド相場 → レンジ相場）を跨ぐと傾向が見える
- 本番昇格前には直近 1 年以上での検証を強く推奨

### 4.5 parentResultId の活用

- サイクル N+1 では N の `id` を `parentResultId` に指定
- 同じ仮説ツリー内で系譜を辿れる（フロントの親 ID リンクで絞り込み可能）
- 別プロファイルを参照する parent も OK（例: `production` → `experiment_...` の系譜）
- 自己参照は 422 で弾かれるが、そもそも ID はサーバ生成なので事故は起きにくい

### 4.6 段階的エスカレーション

| サイクル | レベル | 内容 | 作業量 |
|---|---|---|---|
| 1〜3 | Level 1: パラメータ | 数値調整のみ | JSON 編集のみ |
| 4〜6 | Level 2: 条件組替 | ロジック構造変更 (`signal_rules.*.enabled` 等) | JSON 編集のみ |
| 7〜 | Level 3: 新指標 | Go コード追加 | コード変更 + テスト追加 |

Level 1 で頭打ちになってから Level 2、Level 2 で頭打ちになってから Level 3 に上がる。**飛び越えない**。

---

## 5. アンチパターン / 注意点

### 5.1 やってはいけないこと

- ❌ **`production.json` を直接編集して実験する** — 実験は必ず `experiment_*.json` を作る。`production.json` は承認されたもののみ
- ❌ **複数パラメータを同時に動かす** — 原因分離不能になる
- ❌ **MaxDrawdown 超過でも「他が良いから」で採用する** — 必須制約違反は即 reject
- ❌ **サイクル記録を残さない** — `docs/pdca/YYYY-MM-DD_cycleNN.md` は次の自分/他エージェントが読める唯一の情報源
- ❌ **プロファイルの `indicators.*` を変えた気になる** — 現状 `IndicatorCalculator` がハードコードなので、このセクションは記録用メタデータにしかならない

### 5.2 ハマりやすいポイント

- **個別パラメータと profile の優先順位**:
  - CLI: `flag.Visit` で明示指定したフラグのみ profile を上書き。未指定は profile 値
  - API: リクエストの個別フィールド「非ゼロ値」が profile を上書き。ゼロは「profile 使用」の意味
  - 結果として、CLI で `--stop-loss 0` を明示指定してもゼロが効かないケースがあるので注意
- **プロファイル配置**: `backend/profiles/` 直下に置く。Docker イメージには `Dockerfile` の `COPY --from=backend-builder /app/backend/profiles` で焼き込まれる。追加した JSON は **再ビルドしないとコンテナから見えない**
- **`indicators.bb_squeeze_lookback`**: 現在は `IndicatorCalculator` が固定値 5 で計算するので、このフィールドは記録用メタデータのみ
- **レガシー行の扱い**: PDCA 機能導入前のバックテスト結果は `profile_name=''`, `pdca_cycle_id=''`, `parent_result_id=NULL` で互換性を保つ。フィルタ `profileName=production` にはヒットしない

### 5.3 デバッグのコツ

- API 経由で実行するとプロファイル検証エラーが 400 + 原因メッセージで返る（JSON の文法・`Validate()` 失敗・未知フィールド）
- `DisallowUnknownFields` なのでタイポは即検出される
- `ConfigurableStrategy` が本当に dispatched されているか疑う場合:
  - テスト `TestConfigurableStrategy_EquivalentToDefault` と `TestRunner_ProfileWithDisabledRules_NoTrades` が integration レベルで保証
  - 実 API で profile 指定して `totalTrades` / 結果が変動することを確認
- バックテスト結果 1 行を SQLite で直接見るとき:
  ```bash
  docker compose exec backend sqlite3 /app/backend/data/trading.db \
    "SELECT id, profile_name, pdca_cycle_id, biweekly_win_rate, parent_result_id FROM backtest_results ORDER BY created_at DESC LIMIT 10;"
  ```

---

## 6. 典型的なワークフロー（エージェント実行例）

以下は 1 サイクル分の具体的な作業フロー:

```
# Plan
1. docs/pdca/ で直近のサイクル記録を読む
2. backend/profiles/production.json と直近結果を比較
3. 仮説を立てる: "RSI overbought を 75 → 70 に緩めて contrarian 売りシグナルを増やす"
4. 親 ID を特定: 直近の成功結果の id（例: 01KPCG5WZ7...）

# Do
5. cp production.json experiment_2026-04-17_01.json
6. experiment_2026-04-17_01.json の stance_rules.rsi_overbought を 70 に編集
7. docker compose up --build -d  # プロファイルを焼き込む
8. API で実行:
   curl -X POST http://localhost:38080/api/v1/backtest/run \
     -d '{
       "data": "data/candles_LTC_JPY_PT15M.csv",
       "from": "2025-01-01", "to": "2025-06-30",
       "initialBalance": 100000, "tradeAmount": 0.1,
       "profileName": "experiment_2026-04-17_01",
       "pdcaCycleId": "2026-04-17_cycle01",
       "hypothesis": "RSI overbought 75→70 で contrarian 売り増やす",
       "parentResultId": "01KPCG5WZ7..."
     }'

# Check
9. レスポンスの id, summary を記録
10. MaxDrawdown ≤ 20% を確認。超えたら reject
11. 親結果と TotalReturn / BiweeklyWinRate を比較
12. GET /backtest/results?pdcaCycleId=2026-04-17_cycle01 で一覧確認

# Act
13. docs/pdca/2026-04-17_cycle01.md を _template.md からコピーして結果記録
14. 改善 → 次サイクルで RSI overbought 70 をベースに別方向へ
    悪化 → ロールバック。experiment_...json は残す（学習記録）
15. 頭打ち → Level 2 へ（signal_rules.contrarian.enabled など構造変更）
```

---

## 7. よくある質問

**Q. production.json を編集してしまった。どう戻す？**
A. `git restore backend/profiles/production.json`。Docker イメージへの影響は `docker compose up --build -d`。

**Q. エージェント自身が PDCA を自律的に回して良い？**
A. **コード変更と JSON 作成、バックテスト実行、記録作成までは自律で OK**。本番昇格 (`production.json` の書き換え) は必ず人間承認を挟む設計。

**Q. バックテスト結果を削除したい**
A. `DELETE FROM backtest_results WHERE id = '...';`。親を削除すると子の `parent_result_id` は自動で NULL になる（系譜が切れる）。

**Q. プロファイルの構造が分からない**
A. `backend/profiles/production.json` が生きたリファレンス。Go 構造体は `backend/internal/domain/entity/strategy_config.go`。

**Q. 新しいフィルタを追加したい**
A. `domain/repository/backtest_result.go` の `BacktestResultFilter` にフィールド追加 → `infrastructure/backtest/result_repository.go` の List で動的 WHERE に追加 → ハンドラでクエリパラメータ parse → フロントで UI 露出。

**Q. biweeklyWinRate がずっと 0 になる**
A. 多くは期間不足（< 14 日）か、トレード数不足でカバレッジ < 50%。`totalTrades` を確認。

---

## 8. 次に改善したい箇所（Known TODOs）

### 小さめの UX / 運用改善

- プロファイル名全件 API (`GET /backtest/profiles`) — 現状フロントの絞り込みは現ページ内の distinct のみ
- `hypothesis` のフロント表示（詳細パネル）
- 指標の計算期間 (RSI 14 など) をプロファイル駆動にする — 現状はハードコード。`IndicatorCalculator` の拡張が必要
- CLI の `--pdca-cycle-id` / `--hypothesis` / `--parent-result-id` フラグ — 現状これらは API 経由でしか渡せない
- 本番昇格オペレーションのフロント UI

### PDCA v2 基盤強化（Phase B + PR-7/8 実装済）

2026-04-21 の 20 分 PDCA チャレンジ (`docs/pdca/2026-04-21_promotion_v3.md`) で判明した基盤不足に対する計画を [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../design/2026-04-21-pdca-v2-infrastructure-plan.md) に記載。2026-04-22 時点で以下が merged:

**Phase B + Phase C の実装状況**:

| PR | 内容 | 状況 |
|---|---|---|
| PR-1 | Exit 理由別 / シグナル別サマリ | ✅ merged (#108) |
| PR-2 | 複数期間一括バックテスト + RobustnessScore | ✅ merged (#111) |
| PR-3 | Drawdown 詳細 / Time-in-market / Expectancy | ✅ merged (#112) |
| PR-12 | ATR Trailing Stop | ✅ merged (#113) |
| PR-6 | ADX (+DI/-DI) + per-signal gate | ✅ merged (#114) |
| PR-13 | Walk-forward compute-only | ✅ merged (#115) + TP-wiring fix (#116) + **DB 永続化/GET/CLI (#124)** |
| PR-7 | Stochastics + StochRSI + contrarian gate | ✅ merged (#122) |
| PR-8 | Ichimoku + HTF mode | ✅ merged (#123) |
| **Frontend 露出 (#121)** | FE-A 観測フィールド / FE-B マルチ期間ページ / FE-C ATR フィールド / FE-D ADX パネル / FE-E WFO ページ | ✅ merged (#125-#129) |
| WFO override path 拡張 | Stoch gate を WFO grid から叩けるように | ✅ merged (#130) |

**未実装 (Phase A / C の残)**:
- PR-4: Slippage/Spread 敏感度テスト
- PR-5: Regime 分類（bull-trend/bear-trend/range/volatile）
- PR-9: OBV / CMF
- PR-10: VWAP / Anchored VWAP
- PR-11: Donchian Channel
- PR-14: Regime-conditional プロファイル（PR-5 の上）
- PR-15: 部分約定 / 分割利確 (partial TP)
- PR-16: 動的ポジションサイジング（ATR risk / fractional Kelly）
- `htf_filter.mode` WFO override（現在 `ApplyOverrides` は float64 限定。string 型対応が必要）

### v5 promotion（#118）の現況

- 2026-04-21 v4b で **3yr +9.56% / aggregate geomMean +1.15%** に到達。1yr/2yr はまだマイナス。
- 2026-04-22 cycle22-23（`docs/pdca/2026-04-22_cycle22-23.md`）で PR-7 Stoch gate を 3yr LTC 12/6/6 WFO で評価 → **gate=0 (無効) が 4/4 窓で勝者**、robustness は baseline を下回る。**PR-7 単体では v5 promotion 候補にならない**。
- 2026-04-22 cycle24-27（`docs/pdca/2026-04-22_cycle24-27.md`）で `strategy_risk.stop_loss_percent` grid [3..8] を 3yr LTC 12/6/6 で評価 → **SL=4 が IS 3/4 窓で勝者**（v4b default の SL=5 は 0/4）。TP grid [2..6] は TP=5 が IS 2/4 窓で勝者だが最新窓は v4b の TP=4。実験 profile `experiment_2026-04-22_sl4_only.json` / `..._sl4_tp5.json` を commit 済み。multi-period 比較は **3yr DD が 21.21% → 15〜16% に改善**し初めて 20% 制約を満たすが、**1yr 2024 regime は依然マイナス**で v5 promotion 候補は未成立。
- 2026-04-22 cycle28-37（`docs/pdca/2026-04-22_cycle28-37.md`、**15 分連続スプリント**）で `healthy_v3` 系譜を徹底探索。発見: (1) **healthy_v3 は 2023-04〜2026-03 の単一 regime に curve-fit**（2022 熊相場では −58% 破綻）。(2) `trailing_atr_multiplier` は `TP=4` の healthy_v3 では dead code（TP 先打ち）。(3) `rsi_buy_max=60` が trend_follow IS 3/3 窓勝者（healthy_v3 default 62 より頑健）。(4) `block_counter_trend=false` と `require_macd_confirm=false` は load-bearing — 両方 healthy_v3 の勝因。最終候補 2 本: **攻撃型 `experiment_2026-04-22_sl14_tf60_35`** (gM +16.22%, healthy_v3 超え, SL=14 健全化)、**防御型 `experiment_2026-04-22_sl6_tr30_tp6_tf60_35`** (2022/2023-26 両 regime 生存、SL=6 健全、trailing 発火)。どちらも単独 promotion せず、regime 判定（PR-5）or Ichimoku HTF（PR-8 string override）が次の真の解。
- 次候補: `htf_filter.mode=ichimoku` WFO string override 対応、regime classifier で攻撃/防御 profile 切替。
- 2026-04-22 cycle38（`docs/pdca/2026-04-22_cycle38.md`、**PR feat/wfo-string-override**）で WFO string override を実装済 (`parameterStringGrid` / `ApplyStringOverrides` / `ExpandCombinedGrid`) + Ichimoku HTF mode を cycle28-37 の `sl14_tf60_35` base で検証 → **Ichimoku は 2022 regime を救わない**。 `block_counter_trend=true` にしないと mode が sil-no-op、true にすると全 regime で壊滅。healthy_v3 家系は block=false 依存で HTF filter では 2022 問題に届かない。**Choice A (Ichimoku WFO) は reject、Choice B (Regime classifier) に進む** のが次の自然な一歩。
- 2026-04-22 PR-5 (#133/#134/#135/#136) で Regime classifier + ProfileRouter + 配線 + risk-fix を実装。cycle39（`docs/pdca/2026-04-22_cycle39.md`）で 4 router variants を validation → **router infrastructure は正しく動くが、detector が LTC 15m で常に bull-trend を emit するため override が一度も発火せず**、router profile はすべて default 子と byte-identical。**v5 promotion 候補は出ず**。次 PR (PR-5 part F): `detector_config` を `RegimeRoutingConfig` schema に追加 + WFO override path 対応 → cycle40 で `TrendADXMin` × `VolatileATRPercentMin` を sweep して detector が 2+ regime を emit する閾値を探す。
- 2026-04-22 PR-5 part F (#138) merged。cycle40（`docs/pdca/2026-04-22_cycle40.md`）で **detector_config sweep が complete no-op** と判明: 20 cells (5 ADX × 4 ATR%) すべてが byte-identical、real LTC stream 120k bar の regime histogram を取ると **defaults では range 99.9% / volatile 0.1% / 残り 0% — bull-trend / bear-trend は 0 bar 発火せず**、loose ADX10 でも directional regime は 0%。**LTC 15m で regime routing は構造的に有効化できない**ことを確定。**Decision: regime-routing 撤退**。インフラは main に残し、別 asset/timeframe や別指標軸 (PR-9 OBV, PR-11 Donchian) のために温存。次は cycle28-37 finalist のいずれかを v5 promotion するか、別の indicator 軸を試すかをユーザに判断委ねる段階。

### 配線確認テストの継続

cycle08/09 の「変えても baseline と同じ」罠を再発させないため、新指標を追加する PR は必ず **配線確認テスト** を DoD に含める。現行の参照実装:

- `backend/internal/usecase/strategy/adx_gate_test.go`（PR-6）
- `backend/internal/usecase/strategy/stoch_gate_test.go`（PR-7）
- `backend/internal/usecase/strategy/htf_ichimoku_test.go`（PR-8）

---

## 9. 一次情報へのポインタ

- **PDCA v1 基盤**: #96 → #104 → #98 → #99 → #100 → #101 → #102 → #103（マージ済み）+ #105（Docker fix）
- **PDCA v2 基盤**: #108（PR-1）/ #111（PR-2）/ #112（PR-3）/ #113（PR-12）/ #114（PR-6）/ #115（PR-13）/ #116（PR-13 TP-wiring fix）/ #117（v4b promote）/ #122（PR-7）/ #123（PR-8）/ #124（PR-13 follow-up）/ #125-#129（#121 FE-A~E）/ #130（WFO override path fix）
- **設計書**: `docs/superpowers/specs/2026-04-16-pdca-strategy-optimizer-design.md` / `docs/design/2026-04-21-pdca-v2-infrastructure-plan.md` / `docs/design/plans/2026-04-21-pr*.md`
- **主要テスト**（配線確認＋回帰ガード）:
  - `backend/internal/usecase/strategy/configurable_strategy_test.go` — production.json で DefaultStrategy と等価動作
  - `backend/internal/usecase/strategy/adx_gate_test.go` / `stoch_gate_test.go` / `htf_ichimoku_test.go` — PR-6/7/8 配線確認
  - `backend/internal/usecase/backtest/biweekly_test.go` / `breakdown_test.go` / `drawdown_detail_test.go` / `aggregate_test.go` / `multi_period_runner_test.go` / `walkforward_runner_test.go` / `walkforward_test.go`
  - `backend/internal/infrastructure/database/migrations_test.go` — walk_forward_results / multi_period_results / PDCA カラム + インデックス検証
  - `backend/internal/infrastructure/backtest/result_repository_test.go` / `multi_period_repository_test.go` / `walk_forward_repository_test.go`
  - `backend/internal/interfaces/api/handler/backtest_test.go` / `backtest_multi_test.go` / `backtest_walkforward_test.go` / `backtest_walkforward_persist_test.go`
  - `backend/cmd/backtest/main_test.go` / `walkforward_test.go` — CLI
  - `frontend/src/hooks/__tests__/useBacktest.test.tsx` — クエリ文字列生成
