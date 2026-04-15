# ボリンジャーバンド・ブレイクアウト戦略 + 出来高分析 設計書

**日付:** 2026-04-15
**ステータス:** 承認済み

## Goal

BB スクイーズ後のブレイクアウトを出来高で確認し、自動売買シグナルを生成する新 Stance `BREAKOUT` を追加する。
併せて出来高インジケーターを全 Stance 共通のフィルターとして活用する。

## Background

- 現在のシステムは BB スクイーズ（BBBandwidth < 2%）を検出して HOLD にしているが、スクイーズ後のブレイクアウトは最も利益の出るエントリーポイントであり、見逃している
- 暗号資産は出来高を伴うブレイクアウトが明確に出やすい市場特性がある
- Candle/Ticker に Volume フィールドが既に存在するが、インジケーター計算に未使用

## Architecture

### Approach: 新 Stance `BREAKOUT` + 出来高フィルター

- BB スクイーズ → ブレイクアウト（バンド突破 + 出来高急増）を検出する4つ目の Stance を追加
- 出来高は Volume SMA（20期間）との比率でインジケーター化し、ブレイクアウトの確認条件 + 全 Stance 共通のフィルターとして二重活用
- Stance として独立させることで、既存の TREND_FOLLOW / CONTRARIAN のロジックに影響しない

### 不採用案

- **B. 既存 Stance の拡張のみ**: TREND_FOLLOW が複雑になりすぎる。スクイーズ→ブレイクアウトは独立したパターンであり、無理に既存 Stance に押し込む形になる
- **C. 出来高インジケーターだけ追加**: 自動売買の改善にはつながらない

---

## 1. インジケーター追加

### 新規インジケーター

| インジケーター | 計算方法 | 用途 |
|---|---|---|
| `VolumeSMA20` | 直近20本の出来高の単純移動平均 | 出来高の基準値 |
| `VolumeRatio` | 最新出来高 / VolumeSMA20 | 出来高の相対的な増減（1.0 = 平均、2.0 = 平均の2倍） |

### IndicatorSet への追加フィールド

```go
VolumeSMA20  *float64 `json:"volumeSma20"`  // 出来高20期間SMA
VolumeRatio  *float64 `json:"volumeRatio"`  // 最新出来高 / VolumeSMA20
```

### 計算

既存の `IndicatorCalculator.Calculate()` に追記。キャンドルの `Volume` データは既に取得済みなので、`volumes` スライスを作って SMA を計算する。VolumeRatio は VolumeSMA20 が 0 の場合は nil を返す（ゼロ除算回避）。

---

## 2. Stance Resolver の変更

### 現在のルール

1. RSI < 25 → CONTRARIAN
2. RSI > 75 → CONTRARIAN
3. SMA20 ≈ SMA50（乖離 < 0.1%）→ HOLD
4. それ以外 → TREND_FOLLOW

### 新しいルール（BREAKOUT 追加）

```
1. RSI < 25 or RSI > 75                              → CONTRARIAN（変更なし）
2. BBBandwidth < 0.02（スクイーズ中）:
   a. lastPrice > BBUpper かつ VolumeRatio ≥ 1.5      → BREAKOUT
   b. lastPrice < BBLower かつ VolumeRatio ≥ 1.5      → BREAKOUT
   c. それ以外                                        → HOLD（変更なし）
3. SMA20 ≈ SMA50（乖離 < 0.1%）                      → HOLD（変更なし）
4. それ以外                                           → TREND_FOLLOW（変更なし）
```

### シグネチャ変更

`ResolveAt` に `lastPrice float64` パラメータを追加する。BB ブレイクアウト判定に現在価格が必要なため。

```go
// Before
ResolveAt(ctx context.Context, indicators entity.IndicatorSet, now time.Time) StanceResult

// After
ResolveAt(ctx context.Context, indicators entity.IndicatorSet, lastPrice float64, now time.Time) StanceResult
```

呼び出し元（strategy.go, pipeline, backtest handler）を修正する。

---

## 3. BREAKOUT 戦略ロジック

### evaluateBreakout の判定

| 条件 | アクション |
|---|---|
| lastPrice > BBUpper かつ VolumeRatio ≥ 1.5 | **BUY**（上方ブレイクアウト） |
| lastPrice < BBLower かつ VolumeRatio ≥ 1.5 | **SELL**（下方ブレイクアウト） |
| それ以外 | **HOLD** |

### MACD フィルター（既存パターンと統一）

- BUY 時に histogram < 0 → HOLD（モメンタムが逆方向）
- SELL 時に histogram > 0 → HOLD

### Confidence スコアリング（breakoutConfidence）

| 要素 | ウェイト | 計算 |
|---|---|---|
| 出来高の強さ | 40% | `min((VolumeRatio - 1.0) / 2.0, 1.0)` — 出来高が平均の3倍以上で最大 |
| ブレイクアウトの深さ | 30% | バンドからの乖離率 — BBUpper/BBLower からの距離を BBMiddle で正規化 |
| MACD 確認 | 30% | `min(abs(histogram) / 10, 1.0)` — ヒストグラムの強さ |

---

## 4. 出来高フィルター（全 Stance 共通）

`EvaluateWithHigherTF` の既存フィルターチェーン（BB スクイーズ、MTF フィルター）に追加:

- **VolumeRatio < 0.3**（平均の30%未満）の場合、全 Stance のシグナルを HOLD に変換
- 理由: 出来高が極端に少ない時間帯のシグナルは信頼性が低い
- BREAKOUT は Stance Resolver 段階で VolumeRatio ≥ 1.5 を要求しているので、このフィルターに引っかかることはない

### フィルター適用順序（EvaluateWithHigherTF 内）

1. EvaluateAt でシグナル生成（HOLD ならここで return）
2. **低出来高フィルター** ← 新規
3. BB スクイーズフィルター（TREND_FOLLOW のみ）→ Stance Resolver 側に移動するため削除
4. BB position による Contrarian confidence ブースト（変更なし）
5. MTF フィルター（変更なし）

### BB スクイーズフィルターの責務移動

現在 `EvaluateWithHigherTF` 内にある BB スクイーズ検出（BBBandwidth < 0.02 → HOLD）は、Stance Resolver に移動する。理由:

- スクイーズ中の判定は Stance レベルの関心事（HOLD vs BREAKOUT）
- Strategy Engine はシグナル生成に専念すべき
- Stance Resolver がスクイーズ + ブレイクアウト判定を一箇所で行う方が整合性が高い

---

## 5. 変更ファイル一覧

| ファイル | 変更内容 |
|---|---|
| `entity/indicator.go` | `VolumeSMA20`, `VolumeRatio` フィールド追加 |
| `entity/strategy.go` | `MarketStanceBreakout` 定数追加 |
| `infrastructure/indicator/volume.go` | **新規** — `VolumeSMA`, `VolumeRatio` 計算関数 |
| `infrastructure/indicator/volume_test.go` | **新規** — Volume インジケーターのテスト |
| `usecase/indicator.go` | Volume インジケーター計算を追加 |
| `usecase/stance.go` | BREAKOUT 判定ルール追加、`ResolveAt` に `lastPrice` パラメータ追加 |
| `usecase/strategy.go` | `evaluateBreakout` + `breakoutConfidence` 追加、低出来高フィルター追加、BB スクイーズフィルター削除 |
| `usecase/stance_test.go` | BREAKOUT Stance のテスト |
| `usecase/strategy_test.go` | BREAKOUT シグナル + 低出来高フィルターのテスト |

### 呼び出し元の修正（ResolveAt シグネチャ変更に伴う）

| ファイル | 変更内容 |
|---|---|
| `usecase/strategy.go` | `resolveAt` に `lastPrice` を渡す |
| `usecase/backtest/handler.go` | バックテストからの `ResolveAt` 呼び出しに `lastPrice` を渡す（呼んでいる場合） |

### 既存ロジックへの影響

- 既存の TREND_FOLLOW / CONTRARIAN の判定ロジック自体は変更なし
- BB スクイーズフィルターの責務が strategy.go → stance.go に移動（動作は同等）
- `ResolveAt` のシグネチャ変更は破壊的だが、呼び出し元は限定的
