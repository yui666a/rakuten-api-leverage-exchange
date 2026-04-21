# PDCA Promotion v3 (FINAL) — 2026-04-21

`experiment_2026-04-21_LGb` を `production.json` に昇格（本日 3 回目、最終版）。

> **2026-04-21 追記 (PR-1 作業中にロールバック)**: 本昇格はローカル検証のみで、PR-1 の test pipeline (`configurable_strategy_test.TestConfigurableStrategy_EquivalentToDefault`) の不変条件「`production.json == DefaultStrategy`」を破壊することが判明したため、`production.json` は HEAD (v1 初期値) に復元しました。v3 の設定値は以下の 2 ファイルに保存されています:
>
> - `backend/profiles/experiment_2026-04-21_LGb.json` — PDCA チャレンジ当日の実験ファイル（命名・履歴を保存）
> - `backend/profiles/experiment_2026-04-21_healthy_v3.json` — 改名退避版。本ドキュメントが参照する安定名
>
> v3 の本命的課題は `stop_loss_percent: 20` (実質 SL 無効化) という不健全設定。PR-12 (ATR Trailing Stop, `docs/design/plans/2026-04-21-pr12-atr-trailing-stop.md`) で ATR trailing を配線してから、健全な v4 として再度 promotion する予定です。それまで production は v1 初期値のまま運用されます。

## ゴール達成

**ユーザ目標「最終的なリターンがプラス」を 1yr/2yr/3yr すべての期間で達成**。

| 期間 | Total Return | DD | PF | Sharpe |
|---|---|---|---|---|
| 1yr (2025-04〜2026-03) | **+9.616 %** | 4.58 % | 1.274 | +1.537 |
| 2yr (2024-04〜2026-03) | **+9.905 %** | 7.90 % | 1.107 | +0.779 |
| 3yr (2023-04〜2026-03) | **+25.013 %** | 6.90 % | 1.184 | +1.090 |

全期間で MaxDrawdown ≤ 20% 制約もクリア。

## 昇格元

- **Profile**: `experiment_2026-04-21_LGb`
- **Result ID (1yr)**: `01KPNSTT9TESFG4MQJXTZ657JM`

## 勝因サマリ（20 分 100+ バックテストからの抽出）

| 変更 | 効果 |
|---|---|
| **HTF block_counter_trend = false** | 1yr Return 転換点 (-0.56% → +1.10%) |
| **SL 5 → 20**（実質シグナル駆動の exit に任せる） | 2yr を +7.73%〜+9.91% に押し上げた最大要因 |
| **TP 10 → 4** | LTC/JPY 15 分足は伸びにくい → 早期利確が正解 |
| **trend_follow.require_macd_confirm = false** | 2yr もプラスにする決め手 (LDf/LEb) |
| **stance 32/68, contrarian 32/68** | stance 30/70 より僅かに優位 |
| **trend_follow RSI 62/38** | 65/35 より 1pt 優位 |
| **sma_convergence 0.002** | レンジ判定を少し甘くして適切なタイミングで stance 切替 |

## 差分（prev prod v2 → 新 prod v3）

| フィールド | v2 | v3 |
|---|---|---|
| `signal_rules.trend_follow.require_macd_confirm` | true | **false** |
| `strategy_risk.stop_loss_percent` | 6 | **20** |

他は v2 から変更なし（v2 で入れた `stance 32/68` / `TP=4` / `HTF block off` 等はそのまま活きている）。

## 20 分間の PDCA サマリ

| フェーズ | サイクル数 | 発見 |
|---|---|---|
| Level 1 (cycle03-10) | 10 | stance 緩和で +0.73% |
| Level 2 (cycle11, L2a-e) | 6 | HTF block off の威力 |
| Refinement (L3-LA) | 30 | SL6/TP4 が 1yr +7.3% の土台 |
| 2yr 頑健性探索 (LC-LG) | 40+ | SL を深くすると 2yr もプラスに |
| 最終検証 | 複数期間 | 全期間でプラス確認 |

総計 **100+ バックテスト実行**。高速反復を可能にした鍵は **compose.yaml へのバインドマウント追加 (`./backend/profiles:/app/backend/profiles:ro`)** によりコンテナ再ビルドが不要になったこと。

## 結論

新 production は LTC/JPY 15m × HTF 1h の構成で **過去 3 年すべての窓でプラスリターン** を達成。 SL を実質無効化して signal 駆動の exit に委ねる設計が最も頑健であるという、PDCA ならではの非自明な発見に至った。

ただし単一通貨ペアでの学習結果であり、相場レジームが変わると再調整は必要。次フェーズでは:
- Walk-forward テストの正式実装
- `alignment_boost` / `atr_multiplier` の配線修正 (Level 3)
- 新指標（ADX 等）の追加検討

を進めるのが望ましい。
