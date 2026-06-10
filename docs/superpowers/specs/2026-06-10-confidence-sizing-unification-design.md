# confidence sizing の一元化と意味論の再定義

- **Status:** Proposed
- **Date:** 2026-06-10
- **Related:**
  - PR #154 (ca7c3ad) — 動的ポジションサイジング (entity + 純粋 Sizer)
  - commit 9708110 — signal confidence scoring and amount scaling (confidence scaling の初出)
  - `internal/usecase/positionsize/sizer.go` — 修正対象の本体
  - `cmd/pipeline.go` (legacy) — 重複 `scaleByConfidence` の削除対象

## 背景

初期投資 6 万円・LTC/JPY の本番運用で、`risk_per_trade_pct: 2.0`（資金の 2% をリスクに張る設定）にもかかわらず、実際のポジションが理論値の約半分（0.9 LTC ≒ 6,244 円、`/status` 実測）にとどまる事象を調査した。

原因は **2 つの独立したロット縮小ロジックが二重に効いていた**ことにある。

1. **risk_pct base lot**（`sizer.go:148-150`）: `equity × risk_pct / (entry_price × stop_loss_percent)` で base lot を算出。6 万円 × 2% ÷ 690 ≒ 1.74 LTC。
2. **confidence scaling**（`sizer.go:184-195`）: シグナル確信度が `min_confidence` に近いと `0.5 + 0.5×(conf-min)/(1-min)` で最小 0.5 倍。conf ≈ min のとき **base が ×0.5 され 0.87 LTC**。lot_step 丸めで 0.8〜0.9 LTC。

実測の 0.9 LTC はこの ×0.5 で完全に説明できる。つまり「2% リスク」と設定したのに **実効リスクは約 1%（半分）に落ちていた**。

### confidence scaling の導入経緯（commit 9708110, 2026-04）

> Pipeline filters signals below TRADE_MIN_CONFIDENCE (default 0.3) and **scales order amount linearly between 50%–100% based on confidence.**

導入当初は `tradeAmount`（固定 JPY）× scaling という単純な構造で、「確信度が高いほど大きく張る」という意図は妥当だった。その後 `risk_per_trade_pct`（PR #154）が入り、**同じ scaling 式が sizer 内部にもコピーされた**結果、リスク予算の定義が二重化し、設定値の意味が崩れた。

### 重複コードの所在

confidence scaling の式（`0.5 + 0.5*(conf-min)/(1-min)`）は現在 **2 箇所に同一実装でコピー**されている。

- `internal/usecase/positionsize/sizer.go:192` — sizer 内部（risk_pct モードで効く。ライブで実際に効いている経路）
- `cmd/pipeline.go:709` の `scaleByConfidence` — legacy pipeline 用（EventDrivenPipeline では未使用の死んだコードだが、混乱源として残存）

## 目的

- `risk_per_trade_pct` の意味を明確に再定義し、「設定 = 実挙動」を回復する。
- confidence scaling という機能自体は**残す**（確信度でロットを加減する思想は有効）。ただしリスク予算の計算を **sizer の 1 箇所に集約**し、二重掛けを解消する。
- ハードコードされた floor (`0.5`) を設定項目に昇格させ、scaling の強弱（無効化を含む）を profile で明示制御できるようにする。
- 重複した scaling 式を名前付き関数 1 つに統合し、再びコピーが生まれる余地をなくす。

### 意味論の再定義

> `risk_per_trade_pct` = **確信度が最大 (1.0) のときに賭けるリスク上限**
>
> 実効リスク = `risk_per_trade_pct × confidenceScale(confidence)`
>
> `confidenceScale` = `[min_confidence, 1.0]` → `[confidence_floor, 1.0]` の線形写像

この定義により:

- **設定が嘘をつかない**: 「2%」は「フル確信時に賭けるリスク上限」という明確な意味を持つ。実効 1% は「確信度が低いので上限の半分」と設計上説明がつく。
- **責務が一本化**: リスク予算の計算は sizer の 1 式に集約。
- **floor が明示設定になる**: 現状ハードコードの `0.5` を `confidence_floor` にし、`floor=1.0` で scaling を実質 OFF にできる。ON/OFF が連続的に表現でき、特別扱いが不要。
- **バックテスト実績を壊さない**: `confidence_floor` のデフォルトを 0.5 にすれば現状の振る舞いと数学的に等価。production の promote 実績がそのまま生きる。

## 非目的（明示的に「やらない」）

- **件名2系統の変更**: `reverse_signal`（`real_executor.go` / `simulator.go` の両建て防止ガード）と、production で死んでいる `ExitOnSignal` / `handleExitCandidate` 経路は触らない。AGENTS.md の「1 PR は 1 目的」原則に従う。`reverse_signal` は両建てを物理的に防ぐ構造ガードとして必要であり、`ExitOnSignal` は将来 Decision 駆動 exit を有効化すれば生き返る経路のため、「未使用」コメント追加もしない（誤情報化の懸念）。
- **floor の値変更（実効リスクの引き上げ）**: 今回は `confidence_floor=0.5` で現状等価を維持する。0.5→0.7 等への変更は実効リスクが増えるため、本実装の完了後にバックテスト再キャリブレーションとして別途検討する。
- **confidence を Decision レイヤへ移す**: 「確信度の解釈 = Decision、リスク予算 = sizer」という三層分離の理想形（後述「将来の発展」）は、現状 confidence が単純な線形写像である以上 YAGNI。本実装は将来そこへ発展させる踏み台とする。

## 設計

### コンポーネントと変更点

#### 1. `entity.PositionSizingConfig` に `ConfidenceFloor` を追加

`internal/domain/entity/strategy_config.go`

```go
type PositionSizingConfig struct {
    // ... 既存フィールド ...

    // ConfidenceFloor is the lower bound of the confidence-based lot scale,
    // in [0, 1]. The scale maps [min_confidence, 1.0] → [ConfidenceFloor, 1.0]
    // linearly. 1.0 disables scaling entirely (lot is never reduced by
    // confidence). 0 falls back to the legacy default 0.5 so existing
    // profiles stay bit-identical.
    ConfidenceFloor float64 `json:"confidence_floor,omitempty"`
}
```

- `Validate()` に範囲チェックを追加: `ConfidenceFloor` は `[0, 1]` の範囲（既存の risk_per_trade_pct チェックと同じ箇所、`strategy_config.go:641` 付近）。
- ゼロ値（未設定）は sizer 側で 0.5 にフォールバック → 既存 profile は完全等価。

#### 2. `confidenceScale` 関数を sizer パッケージに新設

`internal/usecase/positionsize/` に名前付き関数として抽出（パッケージ内のファイル。sizer.go 末尾 or 新規 `confidence.go`）。

```go
// confidenceScale maps confidence in [minConfidence, 1.0] to a lot multiplier
// in [floor, 1.0] linearly. It is the single source of truth for how signal
// confidence shrinks the per-trade lot; both the risk_pct path and any legacy
// caller route through here so the formula can never drift between sites.
//
//   - confidence >= 1.0           → 1.0  (full size)
//   - minConfidence not in (0,1)  → 1.0  (scaling disabled; nothing to map)
//   - floor <= 0                  → 0.5  (legacy default)
//   - floor >= 1.0                → 1.0  (scaling disabled by config)
//   - otherwise → floor + (1-floor) * (confidence-minConfidence)/(1-minConfidence)
func confidenceScale(confidence, minConfidence, floor float64) float64
```

- 旧 `0.5 + 0.5*...`（floor 固定 0.5）の挙動は `floor=0.5` を渡せば数学的に等価。
- `floor >= 1.0` で scaling 無効（常に 1.0）を表現。
- 入力クランプ（confidence > 1 → 1, confidence < min → min）は関数内で吸収。

#### 3. `Sizer.Compute` の confidence 適用箇所を置換

`internal/usecase/positionsize/sizer.go:183-195`

現状のインライン式:

```go
if in.MinConfidence > 0 && in.MinConfidence < 1 {
    conf := in.Confidence
    // ... clamp ...
    confScale := 0.5 + 0.5*(conf-in.MinConfidence)/(1.0-in.MinConfidence)
    amount *= confScale
    out.AppliedConf = confScale
}
```

を、関数呼び出しに置換:

```go
floor := s.cfg.ConfidenceFloor // 0 のときは confidenceScale 内で 0.5 にフォールバック
confScale := confidenceScale(in.Confidence, in.MinConfidence, floor)
amount *= confScale
out.AppliedConf = confScale
```

- `out.AppliedConf`（既存の decision-trail フィールド）はそのまま使う。値の意味は変わらない。

#### 4. `cmd/pipeline.go` の重複 `scaleByConfidence` を削除

- `cmd/pipeline.go:709` の `func scaleByConfidence` を削除。
- 同ファイル内の呼び出し（`pipeline.go:415`, `:428`）を確認:
  - **line 415**: risk_pct モードの `baseline`（= RequestedAmount）算出時に掛けている。risk_pct モードでは sizer が base を再計算するため `baseline` は無視される → **scaleByConfidence 掛けは無意味な死んだ計算**。除去（`baseline := snap.tradeAmount / price` に簡素化）。
  - **line 428**: sizer が nil の fixed フォールバック経路（`snap.sizer == nil`）。ここは固定 JPY × scaling という導入当初の正しい構造なので、`positionsize.confidenceScale(..., 0.5)`（legacy floor）に置換して挙動を維持。
- これにより confidence scaling の式は `positionsize.confidenceScale` 1 箇所に集約され、`cmd` パッケージから式のコピーが消える。
- `scaleByConfidence` に言及している既存コメント（`pipeline.go:60`, `:114`）も、`positionsize.confidenceScale` を参照する形に更新する。

### データフロー（ライブ経路）

```
EventDrivenPipeline (cmd/event_pipeline.go)
  └ backtest.RiskHandler.handleNewEntry (handler.go:597-)
        │ synthSignal.Confidence (= decision.Strength)
        │ h.MinConfidence (= profile.MinConfidence, 既定 0.3)
        ▼
     Sizer.Sized → Sizer.Compute (sizer.go)
        ├ base = equity × risk_pct / (entry × sl%)        ← リスク上限
        ├ × ATR scale (任意)
        ├ × confidenceScale(conf, min, floor)  ◀── 新: 1 関数に集約、floor 設定化
        ├ × DD tier scale (任意)
        ├ cap: max_position_pct_of_equity
        └ lot_step 丸め → min_lot ゲート
```

バックテスト経路（`internal/usecase/backtest/runner.go` → 同じ `Sizer.Compute`）も同一関数を通るため、live/backtest の bit-identical 性が維持される（sizer.go 冒頭の設計目標どおり）。

### エラー処理・境界条件

- `confidenceScale` は副作用なしの純粋関数。範囲外入力（負値、NaN を生む 0 除算）は分岐で吸収し、必ず `[floor, 1.0]` または 1.0 を返す。
- `min_confidence` が `(0,1)` の範囲外（0 や 1.0）のとき: scaling は無効（1.0 を返す）。現状コードの `in.MinConfidence > 0 && in.MinConfidence < 1` ガードと同じ意味を関数内に内包する。
- `ConfidenceFloor` 未設定（0）→ 0.5 フォールバックで既存等価。`Validate()` で `[0,1]` 外を拒否。
- profile JSON に `confidence_floor` が無い既存 production.json は、ゼロ値 → 0.5 フォールバックで**完全に従来挙動**。マイグレーション不要。

### テスト戦略

TDD で進める。`confidenceScale` 関数を新設するので、まずその単体テストから書く。

1. **`confidenceScale` 単体テスト**（新規 `confidence_test.go`）
   - `floor=0.5` で旧式と数学的に等価（複数の (conf, min) 点で旧 `0.5+0.5*...` と一致）。← 等価性の要
   - `floor=1.0` → 常に 1.0（scaling 無効）。
   - `floor=0`（未設定相当）→ 0.5 フォールバック。
   - 境界: `conf>=1.0` → 1.0、`conf<min` → floor、`min<=0` / `min>=1` → 1.0。
2. **`Sizer.Compute` の既存テスト**（`sizer_test.go:240 TestSizer_ConfidenceScales`）
   - `ConfidenceFloor` 未指定で従来通りパスすること（回帰）。
   - `ConfidenceFloor=1.0` を渡すと confidence による減衰が消え、base がそのまま出ること（新仕様）。
3. **`entity.Validate` テスト**: `confidence_floor` が `[0,1]` 外でエラーになること。
4. **pipeline.go の死んだ scaleByConfidence 除去**: 既存の pipeline 系テストが緑のままであること（挙動不変の確認）。fixed フォールバック経路のテストがあれば `confidenceScale` 経由でも同値であること。
5. **全体回帰**: `cd backend && go test ./... -race -count=1`。

### ドキュメント更新

- `sizer.go` 冒頭のパッケージコメント（"Signal confidence ... compose multiplicatively"）を、新しい意味論（risk_pct = 上限、confidence は floor までの線形減衰、floor は設定）に合わせて更新。
- `backend/.env.example` / profile スキーマ説明に `confidence_floor` を追記（該当する README / docs があれば）。
- 本設計書を `docs/superpowers/specs/` にコミット。

## 将来の発展（本実装のスコープ外）

confidence の解釈が将来複雑化（非線形、複数指標の加重、regime 依存など）した場合、`confidenceScale` の計算を Decision レイヤ（`decision` パッケージ）へ移し、sizer は事前計算済みの係数のみを受け取る形（当初検討の「案2」）へ発展させられる。本実装で scaling を 1 関数に集約しておくことが、その移設の踏み台になる。

## 受け入れ基準

- [ ] `confidence_floor` 未設定の既存 production.json で、ロット計算が従来と bit-identical。
- [ ] `confidence_floor=1.0` を設定すると、low-confidence シグナルでもロットが base（risk_pct 上限）どおりになる。
- [ ] confidence scaling の式が `positionsize.confidenceScale` の 1 箇所のみに存在する（`cmd/pipeline.go` から式が消える）。
- [ ] `go test ./... -race -count=1` が緑。
- [ ] `reverse_signal` / `ExitOnSignal` 経路は無変更。
