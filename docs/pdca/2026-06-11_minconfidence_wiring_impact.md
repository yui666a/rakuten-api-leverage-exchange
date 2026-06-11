# MinConfidence 配線が backtest 結果に与える影響（記録）

- **Date:** 2026-06-11
- **Context:** confidence sizing 一元化 PR（設計書 `docs/superpowers/specs/2026-06-10-confidence-sizing-unification-design.md` の設計 #5）で、backtest の `RunInput.MinConfidence` をライブ既定（`TRADE_MIN_CONFIDENCE`=0.3）に配線した。これにより backtest でも confidence scaling が有効になる。配線前後の差分を実測し、promote 実績の再検証要否を判断するための記録。

## 計測条件

- CLI backtest（`cmd/backtest run`）
- データ: `candles_LTC_JPY_PT15M.csv`（全期間）+ HTF `candles_LTC_JPY_PT1H.csv`
- プロファイル: `production`（v9、`risk_pct` mode、`risk_per_trade_pct=2.0`、`confidence_floor` 未設定 → 0.5 フォールバック）
- `--initial-balance 60000 --trade-amount 1000`
- 配線前: `TRADE_MIN_CONFIDENCE=0`（confidence scaling 無効。旧 backtest 挙動）
- 配線後: `TRADE_MIN_CONFIDENCE=0.3`（live と同値。confidence scaling 有効）

> 注: この計測は「配線前後の**相対差分**」を見るのが目的。絶対値は production.json コメントの 5 期間検証（期間・条件が異なる）とは直接比較できない。

## 結果

| 指標 | 配線前 (MinConf=0) | 配線後 (MinConf=0.3) | 差分 |
|---|---|---|---|
| Total Return | -6.84% | -10.74% | -3.90pt |
| Max Drawdown | -19.29% | -17.34% | +1.95pt（改善） |
| Profit Factor | 0.96 | 0.92 | -0.04 |
| Sharpe Ratio | -0.14 | -0.38 | -0.24 |
| Total Trades | 733 | 733 | 0（不変） |
| Win Rate | 82.9% | 82.9% | 0（不変） |

## 解釈

1. **トレード数・勝率が不変（733, 82.9%）** → confidence scaling は **エントリー可否（gate）には作用せず、ロットサイズ（anchor）のみ**を変える。設計通り。`MinConfidence` は sizer の scaling anchor として使われ、rejection gate（`Confidence < min` で HOLD）は CLI backtest のこの経路では既に別途効いている（トレード数が変わらないのはそのため）。

2. **リターン低下・DD 改善** → 低確信度シグナルでロットが 0.5〜1.0 倍に縮小するため、トータルのポジション量が減る。結果としてリターンの振れ幅が圧縮され、この期間（全体的に負け気味のレジーム）ではリターンが下がり DD が改善した。これは「確信度の低いトレードを小さく張る」という scaling の設計意図に沿った挙動。

## 含意・フォローアップ

- **promote 実績の再キャリブレーションが必要**: production.json コメントの 5 期間検証（1m +0.2% 〜 2y +67.7%）は、**MinConfidence 配線前の backtest（scaling 無効）で測られた**可能性が高い。配線後は confidence scaling が効くため、同じプロファイルでも数値が変わる。次の PDCA サイクルで production プロファイルを配線後の backtest で再検証し、必要なら `confidence_floor` の調整（設計書の非目的：別タスク）や risk_pct の再選定を行う。
- 本 PR 自体は「配線（live/backtest を同値にする）」までがスコープ。数値を最適化し直すのは別タスク。
- 関連: 設計書「既知の差異・未解決事項」、`[[project_ltc_60k_production]]`。
