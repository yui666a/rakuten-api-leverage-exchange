# Production Promotion v6 — 2026-04-23

- **旧本番**: v5 sl14 (aggressive, promoted 2026-04-22 #141)
- **新本番候補**: v6 = v5 + `bb_squeeze_lookback=3` + `breakout.cmf_buy_min=0.10`
- **in-tree source**: `backend/profiles/experiment_2026-04-22_sl14_bblk3_cmf010.json`

## v5 との差分

| field | v5 (旧) | **v6 (新)** |
|---|---|---|
| `stance_rules.bb_squeeze_lookback` | 5 | **3** |
| `signal_rules.breakout.cmf_buy_min` | 0 (未定義) | **0.10** |

他は完全一致。

## 根拠

### 直近 4 期間 multi-period (3m / 6m / 1y / 2y, to=2026-04-14)

| 期間 | v5 (旧 production) | **v6 (新 production)** | δ |
|---|---|---|---|
| 3m | +0.95% | +0.80% | -0.15pp |
| 6m | +2.60% | +2.38% | -0.22pp |
| 1y | +11.62% | **+11.73%** | +0.11pp |
| 2y | +12.88% | **+15.03%** | **+2.15pp** |
| 2y MaxDD | 6.86% | **6.54%** | **-0.32pp** |
| 4 期間 aggregate geomMean | +6.88% | **+7.32%** | **+0.44pp** |
| aggregate worstDD | 6.86% | **6.54%** | -0.32pp |

**必須制約**:
- ✅ MaxDrawdown ≤ 20% (4 期間すべて)
- ✅ TotalReturn > 0 (4 期間すべて)
- ✅ allPositive (aggregate)

### WFO robustness (IS=6mo / OOS=3mo / step=3mo, 2023-04〜2026-04, 10 windows)

- `cmf_buy_min=0.1` が 2 軸 sweep で **9/10 windows の IS 勝者** — PDCA README 基準 `≥ 6/10` を大きく上回る
- `bb_squeeze_lookback=3` は単独 WFO で 4/10 で regime 依存だが、cmf との組合せで OOS aggregate geomMean +0.87%

### 3y / 4y (2022 regime 含む)

| 期間 | v5 | **v6** | δ |
|---|---|---|---|
| 3y (2023-04〜2026-04) | +28.17% | **+29.63%** | +1.47pp |
| 3y MaxDD | 6.00% | 5.84% | -0.16pp |
| 4y incl 2022 | **−48.31%** | −46.67% | +1.64pp |
| 4y MaxDD | **91.06%** | 90.93% | -0.13pp |

**Known limitation**: 4y (2022 bear incl) 領域は v5/v6 ともに破綻。これは v6 scope 外で、
regime routing (cycle40 撤退) or asset diversification を次の一手とする。

### 10 サイクル連続 reject による local optimum 確認

- cycle46 (rsi_buy_max sweep): reject, combined 超えず
- cycle47 (contrarian rsi): clamp 構造で axis dead、reject
- cycle48 (stance+contrarian 合成): 3m +0.22pp のみ、aggregate reject
- cycle49 (TP×SL×trailing): trailing TP=4/5/6 全 dead, TP=4 が 6/10 robust winner
- cycle50 (defensive base SL×TP incl 2022): regime 問題は SL/TP 調整で救えず
- cycle51 (breakout vol×cmf×htf): 3 種の silent/clamp 発見、本軸 reject
- cycle52 (stance+signal volume 合成): stance 軸 10/10 で 1.5 (現 combined)
- cycle53 (Level 2: contrarian.enabled=false): 1y -14.91pp 大幅悪化
- cycle54 (Level 2: block_counter_trend=true): 2y -17.35pp, DD 13.17% で悪化
- cycle55 (combined sanity re-run): cycle45 と bit-identical, 測定信頼性確保

**2 軸・3 軸・階層 clamp 解消・Level 2 構造組替すべてで reject**。Level 1/2 の hypothesis 空間は探索しつくした。

## 副産物として検出された既知バグ/問題

本 promote とは独立で、次セッションで扱うべき課題:

1. **`htf_filter.alignment_boost` が backtest 経路で silent no-op** (cycle51 発見)
   - `usecase/strategy.go:294` で `signal.Confidence` を boost するだけ
   - `backtest/simulator.go` は `Confidence` を未使用 → backtest で効果なし
   - live trading path で Confidence が使われているかは別途調査が必要
   - production.json では `0.1` のまま維持（v5 と同値）— 変更すると live に影響が出るかもしれないため慎重に
2. **`signal_rules.breakout.volume_ratio_min` は stance 側に clamp される** (cycle51 発見)
3. **`signal_rules.contrarian.rsi_entry/exit` は stance の rsi_oversold/overbought に clamp** (cycle47 発見)
4. **`trailing_atr_multiplier` は TP=4 で dead code** (cycle28-37 / cycle46 / cycle49 で複数回確認)

## Rollback 手順

何か live 運用で問題が発生した場合、以下の 2 ルートで v5 に戻せる:

### Option A: v5 (aggressive) へロールバック
```bash
git checkout main -- backend/profiles/production.json  # このコミットの直前に戻す
# または v5 を直接 copy:
cp backend/profiles/experiment_2026-04-22_sl14_tf60_35.json backend/profiles/production.json
docker compose up --build -d
```

### Option B: defensive プロファイルに切替 (2022 級 bear 対策)
```bash
cp backend/profiles/experiment_2026-04-22_sl6_tr30_tp6_tf60_35.json backend/profiles/production.json
docker compose up --build -d
```
- 2024 bull 相場では機会損失 (2y -3.22%)
- ただし 2022 級 bear を生存 (4y +28.59% vs v5 -48.31%)
- regime 不確実期 / bear 接近観測時に検討

## Same-commit sanity check

cycle55 で combined profile を単独再実行し、cycle45 の数字と bit-identical を確認:
- 3m: 0.007992879450730513 (同一)
- 6m: 0.023789956044947613 (同一)
- 1y: 0.11727340896161878 (同一)
- 2y: 0.15033311663339566 (同一)

→ profile / code / 環境の stability を担保した上での promote。

## Decision rationale

User 指示「4 期間で評価してほしい」の直接回答 = production は全クリア + improvement 候補 combined が存在。
User 指示「もっと攻めて」で 10 サイクル連続 reject → **local optimum 確定**。
これ以上の Level 1/2 微調整は ROI 低下している段階のため、**ここで improvement を取り込み次へ進むのが筋**。

次サイクルは:
- Level 3: 新指標 (VWAP, Anchored VWAP, Regime-conditional profile)
- Asset diversification: BTC 1h / ETH 1h への移植
- silent no-op 4 件の cleanup PR
