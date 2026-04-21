# PR-6: ADX (+DI/-DI) 指標追加

**親計画**: [`docs/design/2026-04-21-pdca-v2-infrastructure-plan.md`](../2026-04-21-pdca-v2-infrastructure-plan.md)
**Phase**: B（新指標追加）
**Stacked PR 順序**: #5 / 6（PR-12 の上）
**見積もり**: 1.5 日

## 動機

20 分 PDCA で最も重要な発見は「**1yr は +7.3%、2yr は -0.9%**」という乖離。2024 年だけ 1 年通してパフォーマンスが落ちる現象を現指標では説明できない。
教科書的には「レンジ相場では trend_follow が機能しない / トレンド相場では contrarian が機能しない」ため、**トレンド強度を測る ADX でゲートを切れば 2 年通した頑健性が上がる可能性が高い**。

## 対象外

- DMI の独立運用（ADX とセットで提供するが主力は ADX）
- 他のトレンド指標（Aroon / Chande Momentum）

## 仕様

### ADX 計算（Wilder 方式）

1. True Range (TR):
   ```
   TR_t = max( High_t - Low_t, |High_t - Close_{t-1}|, |Low_t - Close_{t-1}| )
   ```
2. Directional Movement:
   ```
   +DM_t = High_t - High_{t-1}  if positive and > (Low_{t-1} - Low_t), else 0
   -DM_t = Low_{t-1} - Low_t    if positive and > (High_t - High_{t-1}), else 0
   ```
3. Wilder smoothing (period 14):
   ```
   ATR_t        = Wilder(TR, 14)
   Smoothed_+DM = Wilder(+DM, 14)
   Smoothed_-DM = Wilder(-DM, 14)
   ```
4. DI:
   ```
   +DI = 100 * Smoothed_+DM / ATR
   -DI = 100 * Smoothed_-DM / ATR
   ```
5. DX / ADX:
   ```
   DX_t  = 100 * |+DI - -DI| / (+DI + -DI)
   ADX_t = Wilder(DX, 14)
   ```

### ファイル構成

- `backend/internal/infrastructure/indicator/adx.go` — 計算ロジック
- `backend/internal/infrastructure/indicator/adx_test.go` — golden value test

### IndicatorSet 拡張

```go
// entity/indicator.go
type IndicatorSet struct {
    // ... 既存 ...
    ADX14    *float64 `json:"adx14"`
    PlusDI14 *float64 `json:"plusDi14"`
    MinusDI14 *float64 `json:"minusDi14"`
}
```

データ不足で計算できない場合は nil（既存指標と同方針）。

### IndicatorCalculator への統合

`backend/internal/usecase/indicator.go` で ADX を計算して `IndicatorSet` に詰める。計算最低本数は 2 × period = 28 本。

### Strategy 側の配線

```go
// usecase/strategy.go
type StrategyEngineOptions struct {
    // ... 既存 ...

    // ADX gates
    TrendFollowADXMin  float64  // trend_follow 発動の最低 ADX（デフォルト 20; 0 なら無効）
    ContrarianADXMax   float64  // contrarian 発動の最大 ADX（デフォルト 20; 0 なら無効）
    BreakoutADXMin     float64  // breakout 発動の最低 ADX（デフォルト 15; 0 なら無効）
}

// デフォルト定数
const (
    DefaultTrendFollowADXMin = 20
    DefaultContrarianADXMax  = 20
    DefaultBreakoutADXMin    = 15
)
```

`evaluateTrendFollow` の先頭:
```go
if e.options.TrendFollowADXMin > 0 {
    if ind.ADX14 == nil || *ind.ADX14 < e.options.TrendFollowADXMin {
        return nil, nil
    }
}
```
`evaluateContrarian`:
```go
if e.options.ContrarianADXMax > 0 {
    if ind.ADX14 != nil && *ind.ADX14 > e.options.ContrarianADXMax {
        return nil, nil
    }
}
```
`evaluateBreakout`: 同様に最低 ADX ゲート。

### profile の JSON スキーマ

```json
"signal_rules": {
    "trend_follow": {
        "enabled": true,
        "rsi_buy_max": 62,
        "rsi_sell_min": 38,
        "require_macd_confirm": false,
        "require_ema_cross": true,
        "adx_min": 20
    },
    "contrarian": {
        "enabled": true,
        "rsi_entry": 32,
        "rsi_exit": 68,
        "macd_histogram_limit": 10,
        "adx_max": 20
    },
    "breakout": {
        "enabled": true,
        "volume_ratio_min": 1.5,
        "require_macd_confirm": true,
        "adx_min": 15
    }
}
```

### ConfigurableStrategy 配線

```go
// configurable_strategy.go
engineOpts := usecase.StrategyEngineOptions{
    // ...
    TrendFollowADXMin: profile.SignalRules.TrendFollow.ADXMin,
    ContrarianADXMax:  profile.SignalRules.Contrarian.ADXMax,
    BreakoutADXMin:    profile.SignalRules.Breakout.ADXMin,
}
```

`StrategyProfile` entity にフィールド追加。

### production.json との後方互換

既存 production.json に `adx_min` / `adx_max` が無いため、JSON デコード時は 0（=ゲート無効）になる。従来挙動を維持。
**production.json の自発的な更新は PR-6 ではしない**。PR-6 マージ後に PDCA サイクルで個別に検証して v5 promotion として入れる。

## テスト計画

### Unit（計算）

1. ADX 基本値: 既知入力（TA-Lib / 投資教科書の定番サンプル）で ADX / +DI / -DI がマッチ
2. データ不足: 28 本未満で nil
3. 定常データ: 全バー同じ価格 → ADX = 0
4. 強トレンド: 単調上昇バー → ADX > 40, +DI >> -DI

### Unit（ゲート）

5. `evaluateTrendFollow` ADX < min → nil
6. `evaluateContrarian` ADX > max → nil
7. `evaluateBreakout` ADX < min → nil
8. `adx_min = 0` → ゲート無効で既存挙動

### 配線確認テスト（必須）

9. `TestConfigurableStrategy_ADXGateTakesEffect`
    - profile A: `trend_follow.adx_min: 0`
    - profile B: `trend_follow.adx_min: 30`
    - 同じ CSV で実行して結果が **必ず** 異なる（トレード数が減る）こと
10. `TestConfigurableStrategy_BackwardCompat`
    - production.json（adx_min 無し）が従来挙動と等価（`TestConfigurableStrategy_EquivalentToDefault` の拡張）

### Integration

11. `runner_test.go`: ADX 高閾値（50 など）で trend_follow が 0 件になる

## DoD（as-built）

- [x] indicator 単体 7 ケース (insufficient data / length mismatch / flat / strong up / strong down / range-bound / period=1)
- [x] 配線確認 3 ケース: production.json + `adx_min` オーバーライドで HOLD/通過/ADX 欠損 の 3 分岐
- [x] `TestConfigurableStrategy_EquivalentToDefault` 緑 (production.json の adx_min=0 で gate 無効 → 完全互換)
- [x] strategy_config JSON round-trip に `adx_min` / `adx_max` 追加
- [x] `go test ./... -race -count=1` 全 17 パッケージ緑
- [x] docker e2e で ADX ゲート有効時にトレード数・PF・シグナル分布が有意に変わることを確認 (baseline Trades 1147 → ADX ゲート 395、contrarian 595 → 15 が決定的)

### フォローアップ

- `docs/pdca/agent-guide.md` §2 の主要ファイル表に `adx.go` を追加
- Frontend に ADX14 / +DI14 / -DI14 を表示するパネル (indicator panel 拡張)
- PDCA cycle13+ で ADX ゲート最適プロファイルを探索（2024 年負けの対策）

## ロールバック

- `adx_min = 0` でゲート無効。既存 profile 無傷で動作
- profile schema 変更は任意フィールドのため既存 JSON は無変更で読める

## 備考

- `StrategyProfile` のフィールド追加時は `DisallowUnknownFields` に注意。追加フィールドがあれば profile に無くても default 0 で読めるが、逆に profile に余分なフィールドがあると 400 になる（既存の仕様）
- ADX 単独ではなく **+DI vs -DI の比較** も価値ある。将来 `require_plus_di_above_minus_di` を trend_follow に追加するアイデアはあるが PR-6 スコープ外
- 2024 年の負けが実際にレンジ相場起因か、この PR マージ後の PDCA で検証する
