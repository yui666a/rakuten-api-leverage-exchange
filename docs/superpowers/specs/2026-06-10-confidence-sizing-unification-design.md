# confidence sizing の一元化と意味論の再定義

- **Status:** Implemented (2026-06-11)
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

> `risk_per_trade_pct` = **確信度が最大 (1.0) のときに賭ける per-trade ベース・リスク予算**
>
> 実効リスク = `risk_per_trade_pct × ConfidenceScale(confidence)`（さらに ATR 調整・DD scaling が後段で乗る）
>
> `ConfidenceScale` = `[min_confidence, 1.0]` → `[confidence_floor, 1.0]` の線形写像

この定義により:

- **設定が嘘をつかない**: 「2%」は「フル確信時に賭ける per-trade ベース・リスク予算」という明確な意味を持つ。実効 1% は「確信度が低いのでベース予算の半分」と設計上説明がつく。
- **責務が一本化**: confidence によるリスク予算の減衰計算は sizer の 1 式に集約。
- **floor が明示設定になる**: 現状ハードコードの `0.5` を `confidence_floor` にし、`floor=1.0` で scaling を実質 OFF にできる。ON/OFF が連続的に表現でき、特別扱いが不要。
- **バックテスト実績を壊さない**: `confidence_floor` のデフォルトを 0.5 にすれば現状の振る舞いと数学的に等価。production の promote 実績がそのまま生きる。

> **⚠️ 「上限 (ceiling)」ではなく「ベース予算 (base budget)」である点に注意**（Codex レビュー指摘1）。
> `risk_per_trade_pct` で求めた base lot には、後段で `ATRAdjust`（最大 ×2.0、`sizer.go:156-179`）や DD scaling が**乗算**され得る。
> また `risk.go` の per-trade リスクは hard ceiling として検証されていない（名目額/残高比チェックのみ）。
> したがって実現リスクが `risk_per_trade_pct` を**上回る**ケースが構造的に存在する。
> 本タスクは confidence scaling の一元化に集中し、per-trade リスクの hard ceiling 化は**スコープ外**（別タスク。後述「既知の差異・未解決事項」参照）。
> production.json は現状 `atr_adjust` 未設定（ATR 係数 = 常に 1.0）のため、この乖離は現時点では顕在化していない。

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

    // ConfidenceFloor is the lower bound of the confidence-based lot scale.
    // Effective range is (0, 1]; the scale maps [min_confidence, 1.0] →
    // [ConfidenceFloor, 1.0] linearly. 1.0 disables scaling entirely (lot is
    // never reduced by confidence). 0 is the "unset" sentinel — because
    // omitempty cannot tell an explicit 0 from a missing field, 0 always
    // falls back to the legacy default 0.5 so existing profiles stay
    // bit-identical. To minimise the floor explicitly, set a small positive
    // value (e.g. 0.01), not 0.
    ConfidenceFloor float64 `json:"confidence_floor,omitempty"`
}
```

- `Validate()` に範囲チェックを追加: `ConfidenceFloor` は `[0, 1]`（負値・1 超を拒否、`0` は unset として許容）。既存の risk_per_trade_pct チェックと同じ箇所（`strategy_config.go:641` 付近）。
- ゼロ値（未設定）は sizer 側で 0.5 にフォールバック → 既存 profile は完全等価。

#### 2. `ConfidenceScale` 関数を sizer パッケージに新設（**公開関数**）

`internal/usecase/positionsize/` に名前付き関数として抽出（パッケージ内のファイル。新規 `confidence.go`）。

> **⚠️ 公開関数 (`ConfidenceScale`、大文字始まり) にする**（Codex レビュー指摘5）。
> `cmd/pipeline.go`（`main` パッケージ）から `positionsize.ConfidenceScale(...)` として呼ぶため、
> Go の可視性ルール上、非公開 (`confidenceScale`) では別パッケージから参照できない。
> 「パッケージ内に閉じる」という当初案は実装不能。export が必須。

```go
// ConfidenceScale maps confidence in [minConfidence, 1.0] to a lot multiplier
// in [floor, 1.0] linearly. It is the single source of truth for how signal
// confidence shrinks the per-trade lot; both the risk_pct path (Sizer.Compute)
// and the legacy fixed-amount caller (cmd/pipeline.go) route through here so
// the formula can never drift between sites.
//
//   - confidence >= 1.0           → 1.0  (full size)
//   - minConfidence not in (0,1)  → 1.0  (scaling disabled; nothing to map)
//   - floor <= 0                  → 0.5  (legacy default)
//   - floor >= 1.0                → 1.0  (scaling disabled by config)
//   - otherwise → floor + (1-floor) * (confidence-minConfidence)/(1-minConfidence)
func ConfidenceScale(confidence, minConfidence, floor float64) float64
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
floor := s.cfg.ConfidenceFloor // 0 のときは ConfidenceScale 内で 0.5 にフォールバック
confScale := ConfidenceScale(in.Confidence, in.MinConfidence, floor)
amount *= confScale
out.AppliedConf = confScale
```

- `out.AppliedConf`（既存の decision-trail フィールド）はそのまま使う。値の意味は変わらない。

#### 4. `cmd/pipeline.go` の重複 `scaleByConfidence` を削除

- `cmd/pipeline.go:709` の `func scaleByConfidence` を削除。
- 同ファイル内の呼び出し（`pipeline.go:415`, `:428`）を確認:
  - **line 415**: risk_pct モードの `baseline`（= RequestedAmount）算出時に掛けている。risk_pct モードでは sizer が base を再計算するため `baseline` は無視される → **scaleByConfidence 掛けは無意味な死んだ計算**。除去（`baseline := snap.tradeAmount / price` に簡素化）。
  - **line 428**: sizer が nil の fixed フォールバック経路（`snap.sizer == nil`）。ここは固定 JPY × scaling という導入当初の正しい構造なので、`positionsize.ConfidenceScale(..., 0.5)`（legacy floor）に置換して挙動を維持。
- これにより confidence scaling の式は `positionsize.ConfidenceScale` 1 箇所に集約され、`cmd` パッケージから式のコピーが消える。
- `scaleByConfidence` に言及している既存コメント（`pipeline.go:60`, `:114`）も、`positionsize.ConfidenceScale` を参照する形に更新する。

#### 5. backtest 経路に `MinConfidence` を配線する

**問題**: `RunInput.MinConfidence` は runner が受け取り sizer に渡す配線（`runner.go:255`）はあるが、API/CLI ハンドラ側で `MinConfidence:` をセットする箇所が存在せず、backtest では実質 `MinConfidence=0` で走る。すると `ConfidenceScale` の `min ∈ (0,1)` ガードで scaling が**無効化**され、ライブ（`min=0.3` で有効）と backtest で confidence scaling の効きが食い違う。これを直さないと、`ConfidenceScale` に一元化しても「live/backtest で confidence scaling 同値」は達成できない。

**方針**: backtest の `RunInput.MinConfidence` を、ライブと同じ既定値 `0.3`（`TRADE_MIN_CONFIDENCE` 既定）に揃える。配線元は次のいずれか — 実装計画で確定する:

- **(a) 環境変数から**: backtest 実行時も `cfg.Trading.MinConfidence`（`config.go:127-132`）を読み、`RunInput.MinConfidence` に設定する。ライブと完全同一ソース。CLI/API 両経路で共通の config を参照できる場合に最もシンプル。
- **(b) profile から**: profile に `min_confidence` を持たせ、backtest はそこから読む。ただし現状 profile に `MinConfidence` フィールドは無く（`strategy_config.go`）、新設が必要。ライブも将来 profile 由来に寄せるなら一貫するが、本タスクの範囲が広がる。
- **(c) リクエストパラメータ**: backtest API/CLI に `min_confidence` を明示パラメータとして追加し、未指定時は 0.3 にフォールバック。

**推奨は (a)**: ライブと同一ソース（環境変数）を使い、最小の新規概念で live/backtest 同値を達成できる。profile スキーマ変更（b）は「confidence を Decision/profile へ寄せる」将来の発展と一緒にやるべきで、本タスクでは過剰。

**影響の注意**: この配線により backtest の振る舞いが変わる（これまで scaling 無効だった backtest が有効になる）。promote 済みプロファイルの実績値（production.json のコメントにある 5 期間検証など）は **scaling 無効の backtest で測られていた可能性**があり、配線後は再検証が必要になり得る。実装計画で「配線前後の backtest 結果差分を確認」するステップを必ず置く。

### データフロー（ライブ経路）

```
EventDrivenPipeline (cmd/event_pipeline.go)
  └ backtest.RiskHandler.handleNewEntry (handler.go:597-)
        │ synthSignal.Confidence (= decision.Strength)
        │ h.MinConfidence (= cfg.Trading.MinConfidence, 環境変数 TRADE_MIN_CONFIDENCE, 既定 0.3)
        ▼
     Sizer.Sized → Sizer.Compute (sizer.go)
        ├ base = equity × risk_pct / (entry × sl%)        ← ベース・リスク予算
        ├ × ATR scale (任意, 最大 ×2.0 — base を上回り得る)
        ├ × ConfidenceScale(conf, min, floor)  ◀── 新: 1 関数に集約、floor 設定化
        ├ × DD tier scale (任意)
        ├ cap: max_position_pct_of_equity
        └ lot_step 丸め → min_lot ゲート
```

`Sizer.Compute` 自体は live/backtest で同一関数のため、**confidence scaling 計算に関しては** bit-identical（sizer.go 冒頭の設計目標どおり）。sizer の**入力**のうち、**MinConfidence の配線差異は本タスクのスコープ内で解消する**（設計 #5）。残る入力差異（DD scaling の peak equity 初期化、後段の BookGate / ATR ストップ距離）は本タスクのスコープ外で、「bit-identical」の限界として「既知の差異・未解決事項」に明記する。

### エラー処理・境界条件

- **`MinConfidence` の出所と役割**: `MinConfidence` は **profile ではなく** `cfg.Trading.MinConfidence`（環境変数 `TRADE_MIN_CONFIDENCE`、既定 0.3、`config.go:127-132`）由来。live は `main.go:189-190` → `event_pipeline.go:762-767`、backtest は `RunInput.MinConfidence`（後述「既知の差異⑤」）経由で sizer に渡る。役割は **2 つ**: (a) **rejection gate** — `Confidence < MinConfidence` のシグナルは `NEW_ENTRY` 前に HOLD で弾く（`pipeline.go:375` 相当）。(b) **scaling anchor** — 通過したシグナルの `[MinConfidence, 1.0]` を `[floor, 1.0]` に写像する起点。`ConfidenceScale` が使うのは (b) の意味。
- `ConfidenceScale` は副作用なしの純粋関数。範囲外入力（負値、NaN を生む 0 除算）は分岐で吸収し、必ず `[floor, 1.0]` または 1.0 を返す。
- `min_confidence` が `(0,1)` の範囲外（0 や 1.0）のとき: scaling は無効（1.0 を返す）。現状コードの `in.MinConfidence > 0 && in.MinConfidence < 1` ガードと同じ意味を関数内に内包する。
- **`ConfidenceFloor` の有効範囲は `(0,1]`、`0 = 未設定` センチネル**: `json:"confidence_floor,omitempty"` では「明示的な 0」と「未指定」を JSON 上区別できないため、**floor=0 を有効な設定値とはしない**（floor=0 は「確信度ゼロでロットゼロ」を意味し実用上無意味）。`0` は常に「未設定 → legacy default 0.5 にフォールバック」と解釈する。`Validate()` は `[0,1]` 外（負値・1 超）を拒否し、`0` は許容（=unset 扱い）。floor を明示的に最小化したい場合は `0` ではなく小さな正値（例 0.01）を設定する。
- profile JSON に `confidence_floor` が無い既存 production.json は、ゼロ値 → 0.5 フォールバックで**完全に従来挙動**。マイグレーション不要。

### テスト戦略

TDD で進める。`ConfidenceScale` 関数を新設するので、まずその単体テストから書く。

1. **`ConfidenceScale` 単体テスト**（新規 `confidence_test.go`）
   - `floor=0.5` で旧式と数学的に等価（複数の (conf, min) 点で旧 `0.5+0.5*...` と一致）。← 等価性の要
   - `floor=1.0` → 常に 1.0（scaling 無効）。
   - `floor=0`（未設定相当）→ 0.5 フォールバック。
   - 境界: `conf>=1.0` → 1.0、`conf<min` → floor、`min<=0` / `min>=1` → 1.0。
2. **`Sizer.Compute` の既存テスト**（`sizer_test.go:240 TestSizer_ConfidenceScales`）
   - `ConfidenceFloor` 未指定で従来通りパスすること（回帰）。
   - `ConfidenceFloor=1.0` を渡すと confidence による減衰が消え、base がそのまま出ること（新仕様）。
3. **`entity.Validate` テスト**: `confidence_floor` が `[0,1]` 外でエラーになること。
4. **pipeline.go の死んだ scaleByConfidence 除去**: 既存の pipeline 系テストが緑のままであること（挙動不変の確認）。fixed フォールバック経路のテストがあれば `ConfidenceScale` 経由でも同値であること。
5. **backtest MinConfidence 配線テスト**: backtest 実行で `RunInput.MinConfidence` が 0.3（ライブ既定）に設定され、sizer の confidence scaling が有効になること。配線前は scaling 無効（係数 1.0）、配線後は `min=0.3` 基準で減衰がかかることを、同一シグナル列で比較するテスト。
6. **配線前後の backtest 結果差分の記録**: production.json のプロファイルで、MinConfidence 配線の前後でバックテスト結果（リターン/MaxDD/トレード数）がどう変わるかを実測し、設計書 or PDCA ノートに記録（promote 実績の再検証要否を判断するため）。
7. **全体回帰**: `cd backend && go test ./... -race -count=1`。

### ドキュメント更新

- `sizer.go` 冒頭のパッケージコメント（"Signal confidence ... compose multiplicatively"）を、新しい意味論（risk_pct = per-trade ベース・リスク予算、confidence は floor までの線形減衰、floor は設定）に合わせて更新。実装コメントに「上限 / ceiling」の語を持ち込まない（ATR/DD が後段で乗算され base を上回り得るため）。
- `backend/.env.example` / profile スキーマ説明に `confidence_floor` を追記（該当する README / docs があれば）。
- 本設計書を `docs/superpowers/specs/` にコミット。

## 既知の差異・未解決事項（本タスク非スコープ）

Codex レビューで、設計書の「live/backtest bit-identical」「risk_pct = リスク上限」という主張が、confidence sizing **とは別の既存要因**で既に成立していないことが判明した。いずれも本タスクが新たに壊すものではなく、confidence scaling 一元化の前後で挙動は変わらない。本タスクのスコープ外として記録し、必要なら別タスクで扱う。

1. **per-trade リスクの hard ceiling が無い**（指摘1, 重要度: 中）
   `risk_per_trade_pct` で求めた base lot に ATRAdjust（最大 ×2.0）/ DD scaling が乗算され、`risk.go:157-195` は名目額/残高比のみ検証し per-trade ストップリスクを上限として強制しない。実現リスクが `risk_per_trade_pct` を超え得る。→ 本設計書では意味論を「ベース予算」に改めて乖離を正直に記述した。hard ceiling 化は別タスク。production は `atr_adjust` 未設定のため現状は顕在化しない。

2. **Sizer の想定ストップ距離と TickRiskHandler の実ストップ距離が乖離**（指摘2, 重要度: 中）
   `Sizer.Compute` はリスク予算を `Input.StopLossPercent` だけで計算するが、`TickRiskHandler.stopLossDistance`（`backtest/handler.go:925-938`）は `max(entry × stopLossPercent, currentATR × stopLossATRMultiplier)` を使う。ATR が広い局面では実決済距離がサイザー想定を超え、実現リスクが設計値を超過する。→ サイザーとリスク監視のストップ距離整合は別タスク。production は `stop_loss_atr_multiplier: 0` のため percent のみで一致し、現状は顕在化しない。

3. **BookGate のフラグが live/backtest で非対称**（指摘3, 重要度: 低）
   `AllowOnMissingBook` がバックテスト `false` / ライブ `true`（`runner.go` vs `event_pipeline.go`）。板スナップショット欠損時の通過可否が分岐し、同一パスでも結果が異なり得る。これは confidence sizing と無関係の既存差異。→ [[project_bookgate_disabled]] とあわせて別途整理。

4. **DD scaling の peak equity 初期化が live/backtest で異なる**（指摘4, 重要度: 低）
   バックテストは `PeakTracker(InitialBalance)`、ライブは `rm.LocalBalance()` 由来かつ `initialBalance > 0` のときだけ Peak 設定。同一パラメータでも DD scaling が diverge し得る。→ peak equity 計算の統一は別タスク。

> **補足**: かつてここに挙げていた「backtest 経路で `MinConfidence` がほぼ未配線」の項目は、本タスクの**スコープ内**に格上げした（後述「設計 #5」）。confidence scaling の一元化と地続きで、これを直さないと backtest で scaling が無効のままライブと食い違うため。

## 将来の発展（本実装のスコープ外）

confidence の解釈が将来複雑化（非線形、複数指標の加重、regime 依存など）した場合、`ConfidenceScale` の計算を Decision レイヤ（`decision` パッケージ）へ移し、sizer は事前計算済みの係数のみを受け取る形（当初検討の「案2」）へ発展させられる。本実装で scaling を 1 関数に集約しておくことが、その移設の踏み台になる。

## 受け入れ基準

- [ ] `confidence_floor` 未設定の既存 production.json で、ロット計算が従来と bit-identical。
- [ ] `confidence_floor=1.0` を設定すると、low-confidence シグナルでもロットが base（risk_pct ベース予算）どおりになる。
- [ ] confidence scaling の式が `positionsize.ConfidenceScale` の 1 箇所のみに存在する（`cmd/pipeline.go` から式が消える）。
- [ ] backtest 実行で `RunInput.MinConfidence` がライブ既定（0.3）に配線され、live/backtest で confidence scaling が同値で効く。
- [ ] MinConfidence 配線前後の backtest 結果差分を実測・記録し、promote 実績の再検証要否を判断した。
- [ ] `go test ./... -race -count=1` が緑。
- [ ] `reverse_signal` / `ExitOnSignal` 経路は無変更。
