# Exit-on-Signal — Promote 見送り判断

**Date**: 2026-05-03
**続編**: `docs/pdca/2026-05-03_exit_on_signal_ab.md` (初回 A/B、spread=0.1% default のみ)
**結論**: **`exit_on_signal=true` を `production_ltc_60k` に適用しない**

## 判断の経緯

PR #239 で初回 A/B (spread=0.1%) は全 4 期間で on が完勝した。promote 前 TODO として「手数料の現実的な織り込み」を挙げ、**spread sensitivity** で再検証した結果、**on は spread 30bps 以上で off に逆転**することを確認。LTC/JPY の現実 spread は 30〜80bps が想定されるレンジで、**実運用では悪化要因**になる確度が高い。

## Spread sensitivity (3m / 1y, off vs on)

| 期間 | spread | off Return / DD | on Return / DD | δ Return | 判定 |
|---|---|---|---|---|---|
| 3m | 0.1% | -0.30% / 1.11% | **+0.83% / 1.14%** | +1.13pp | on 勝ち |
| 3m | 0.2% | -0.33% / 1.12% | **+0.16% / 1.32%** | +0.49pp | on やや勝ち |
| 3m | 0.3% | **+0.88% / 0.75%** | -0.30% / 1.29% | -1.18pp | **off 勝ち** |
| 3m | 0.5% | **+0.79% / 0.77%** | -1.59% / 2.22% | -2.38pp | **off 圧勝** |
| 1y | 0.1% | -1.82% / 4.36% | **+2.40% / 2.64%** | +4.22pp | on 勝ち |
| 1y | 0.2% | -2.13% / 4.60% | **-0.05% / 3.23%** | +2.08pp | on 勝ち (両方負け) |
| 1y | 0.3% | **-1.20% / 4.73%** | -2.18% / 4.24% | -0.98pp | **off 勝ち** |
| 1y | 0.5% | **-1.78% / 4.98%** | -6.79% / 7.40% | -5.01pp | **off 圧勝** |

**break-even は spread ≈ 25bps 付近**。spread 30bps 以上では確定的に off が勝つ。

## なぜ逆転するのか

`exit_on_signal=true` は **trades を 5〜13 倍に増やす** (3m 10→150, 1y 86→575)。
- Decision-driven exit 自体は SL 回避により net positive (前 PDCA で確認済)
- しかし **trade 1 件あたり spread コスト ≈ 0.1〜0.2% が均等にかかる**
- **trades × spread が増分メリットを侵食する** 構造

**spread コストが Return に占める比率** (1y / on で見ると):

| spread | spread cost ¥ | initial の % |
|---|---|---|
| 0.1% | ~600 | ~1.0% |
| 0.2% | 2,931 | 4.9% |
| 0.3% | 4,308 | 7.2% |
| 0.5% | 6,959 | 11.6% |

trades=575 件で spread 0.5% の場合、**6.9% を spread に持っていかれる**。これは Phase 1 で確認した +4.2pp の改善幅を完全に飲み込む。

## LTC/JPY 現実 spread

楽天 Wallet 証拠金 LTC/JPY の現実 spread (best bid / best ask の中点距離) は手元のデータでは正確に測れていないが、以下が想定根拠:

- 過去 production v1 promotion (cycle70-71) は spread=0.1% default で評価し、live でも安定運用 → 0.1〜0.2% は「実運用上は許容できる近似」
- ただし trades が少なく（年間 ~80 件）、spread の影響度が小さかった
- exit_on_signal=true は trades を **桁違いに増やす** (~600 件/年) ため、spread の影響度が支配的になる

**結論**: spread が 25bps 以上あれば on は劣後する。production を 0.1〜0.2% spread 前提で promote すると過剰最適化になる。

## 判断: promote 見送り

- `production_ltc_60k.json` は **`exit_on_signal=false` のまま維持** (= 何も変更しない)
- 実装 PR #238 (`feat(risk): exit_on_signal`) は main に残す。プロファイル opt-in なので production の挙動は変わらない
- experiment プロファイル (PR #239) も残す。将来 spread 観測 / orderbook データ充実時の再評価素材として残す

## 副産物 / 学び

1. **spread sensitivity の重要性**: 初回 PDCA の spread=0.1% default は「PDCA をすばやく回すための便宜」だが、trades 増を伴う変更には不適。**trades が桁違いに変わる変更は最低 spread {0.1, 0.3, 0.5}% で検証する** ルールを今後採用
2. **break-even spread** という指標が判断材料として強い。「spread X bps なら on」「Y bps なら off」と分岐がきれいに見えた
3. **EntryCooldownSec sweep は実施せず**: promote 判定が negative なので意味がない。サンクコスト回避

## 次に何をすべきか

短期:
- 何もしない (現 production を維持)
- 実装は残るので future にとっておく

中期 (実 spread 計測のため):
- LTC/JPY の **実 spread を tick データから測定**するスクリプト (median / 95th percentile)
- 結果を `docs/pdca/_lookups/ltc_jpy_spread_estimate.md` 等に残す
- もし実 spread が 20bps 以下と確定すれば、改めて promote 検討
- 25bps 以上なら永続的に off が正

長期 (signal を改良する方向):
- `exit_on_signal=true` の本質的な効果 (SL 回避 + TP 機会保持) は他の手段でも実現可能
- 例: Trailing Stop の閾値調整、Strategy 側で「逆シグナル時は size を小さくする (反転 hedging)」 etc.
- これらは spread 中立 (trades が増えない)

## 参照ファイル

- 初回 A/B レポート: `docs/pdca/2026-05-03_exit_on_signal_ab.md`
- 実装 PR: #238 (main mergeed)
- Experiment profile: `backend/profiles/experiment_ltc_60k_exit_on_signal.json`
- Result JSONs (ローカルのみ):
  - spread 0.1%: `/tmp/bt_{off,on}_{3m,6m,1y,2y}.json`
  - spread 0.2%: `/tmp/bt_{off,on}_{3m,1y}_s02.json`
  - spread 0.3%: `/tmp/bt_{off,on}_{3m,1y}_s03.json`
  - spread 0.5%: `/tmp/bt_{off,on}_{3m,1y}_s05.json`
