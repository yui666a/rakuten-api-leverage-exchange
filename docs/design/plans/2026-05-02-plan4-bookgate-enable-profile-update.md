# PR4 Plan: BookGate 有効化 + プロファイル更新 + EntryCooldown プロファイル化

- 作成日: 2026-05-02
- 親設計書: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
- 前段 PR: #232 / #233 / #234 (Phase 1 PR1〜PR3 全マージ済み)
- スコープ: Phase 1 / Stacked PR シリーズ (PR4 ÷ 5)
- 動作変更あり: BookGate が production_ltc_60k で発動し始める。EntryCooldown も有効化

---

## 0. このドキュメントの位置付け

PR3 で実発注経路は新ルートに切り替わったが、**BookGate と EntryCooldown は設定が無いため依然として無効**。設計書 §2.2「BookGate (`booklimit.Gate`) を production_ltc_60k で有効化する」を満たすため、profile 経由でこれらを設定可能にし、`production_ltc_60k.json` に保守的初期値を入れる。

PR4 マージ時点の **観測可能な変化**:

- `StrategyProfile.Risk` に 3 フィールド (`MaxSlippageBps` / `MaxBookSidePct` / `EntryCooldownSec`) が増える
- profile 読み込み時に `RiskManager.UpdateConfig` 経由で値が反映される
- `production_ltc_60k.json` に保守的な初期値 (例: bps=15, sidepct=20, cooldown=60) が入る
- 板薄時間帯に BookGate が REJECTED を出し始める（=「両建てバグ修正で見えなくなっていた」エッジケース対応）
- close 約定後 60 秒は新規エントリーが COOLDOWN_BLOCKED

---

## 1. 設計書からの調整点

### 1.1 現状コードの構造を踏まえた配線

PR4 開始時点の事実：

- `entity.RiskConfig` には `MaxSlippageBps` / `MaxBookSidePct` / `EntryCooldownSec` 全部存在（PR3 までで揃った）
- live event_pipeline.go と backtest runner.go の **両方で BookGate は条件付き配線済み** — `cfg.MaxSlippageBps > 0 || cfg.MaxBookSidePct > 0` で activate
- ただし **profile JSON には対応フィールドが無い** — `cfg.Risk` (env var only) からしか値が入らない
- main.go は profile 読み込み **後** に riskMgr を構築し直すパスが無い: `riskMgr := NewRiskManager(...)` の後で `loadLiveProfile()` が呼ばれる

→ 必要な改修：

1. `StrategyRiskConfig` (profile 内) に 3 フィールド追加
2. main.go の起動順序を「profile → riskMgr」または `riskMgr.UpdateConfig` 呼び出しに変える
3. `production_ltc_60k.json` を更新

### 1.2 設定の優先順位

env var (`RISK_MAX_SLIPPAGE_BPS` など) も既に存在する。両方ある場合の優先順位：

- **profile が値を持っていれば profile 優先**（profile-driven な意図が prod の真実）
- profile に値が無い (zero) なら env var 値を fallback
- どちらも 0 なら gate 無効（既存動作）

これにより、PR4 後でも env var で動かしている古い deployment は壊れない。`production.json` 等の他 profile は値未設定のまま残し、cooldown / BookGate は無効のまま（後続 PR5 まではそのまま）。

### 1.3 `production_ltc_60k.json` の初期値

設計書 §10.2 に sweep 候補が載っている：

- `entry_cooldown_sec`: 30 / 60 / 120 / 300
- `max_slippage_bps`: 10 / 15 / 20 / 30
- `max_book_side_pct`: 10 / 20 / 30

PR4 ではこの sweep を**走らせない**。「保守的初期値で発火確認」が PR4 のゴール。設計書 §8.4 の例 (`bps=15`, `sidepct=20`, `cooldown=60`) を採用し、PDCA で sweep するのは別 PR / 別作業。

### 1.4 MCP / API 経由の `UpdateConfig`

`RiskManager.UpdateConfig` は API/MCP からも呼ばれる。MCP が **古いフィールドだけ送ってくる** と、PR4 で profile 経由で入れた新フィールドが消える可能性がある。

実装上は `UpdateConfig(config entity.RiskConfig)` が **構造体丸ごと置換** か **0 値スキップ** かで挙動が変わる。これは現状コードを読んで判断する：

<details>
<summary>確認手順</summary>

```go
// usecase/risk.go の UpdateConfig
func (rm *RiskManager) UpdateConfig(config entity.RiskConfig) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    rm.config = config // 丸ごと置換 → MCP が部分更新したつもりで全 reset
}
```

もし丸ごと置換なら、MCP/API ハンドラ側でマージするか、`UpdateConfig` を `partial` 仕様にする小改修が必要。本 plan §3 Task 5 で対応する。
</details>

### 1.5 backtest 側の検証

backtest runner は既に `riskCfg.MaxSlippageBps > 0 || MaxBookSidePct > 0` で BookGate 配線。`buildRunInput`（cmd/backtest/main.go）が profile から RiskConfig を組み立てる経路で 3 フィールドを引き継げば、CLI 経由の backtest でも自動で gate が効く。API 経由の backtest (`/backtest/run`) も同じ path を通るので一括で対応可能。

---

## 2. ファイル変更マップ

| ファイル | 変更 | 行数目安 |
|---|---|---|
| `backend/internal/domain/entity/strategy_config.go` | StrategyRiskConfig に 3 フィールド + Validate 拡張 | +20 |
| `backend/internal/domain/entity/strategy_config_test.go` | Validate ケース追加 | +50 |
| `backend/cmd/main.go` | profile → riskMgr.UpdateConfig 反映 + selector func 追加 | +30 |
| `backend/cmd/main_test.go` (もしあれば) | profile 反映テスト | +30 |
| `backend/cmd/backtest/main.go` | buildRunInput で profile → RiskConfig マージ | ~10 行差分 |
| `backend/internal/interfaces/api/handler/backtest.go` | API backtest が profile の 3 フィールドを RiskConfig に流す | ~15 行差分 |
| `backend/internal/usecase/risk.go` | UpdateConfig が部分マージできるよう改修 (or 既に丸ごと置換なら別ヘルパー追加) | +15 |
| `backend/internal/usecase/risk_test.go` | UpdateConfig 部分マージテスト | +30 |
| `backend/internal/usecase/booklimit/book_limit_test.go` | エッジケース拡充 (snapshot stale / empty / top-N 不足) | +80 |
| `backend/profiles/production_ltc_60k.json` | 3 フィールド追加 (bps=15, sidepct=20, cooldown=60) | +5 |

合計：新規 0、編集 9、約 +280 行。

---

## 3. 実装タスク

### Task 1: StrategyRiskConfig に 3 フィールド追加

**変更**: `entity/strategy_config.go`

```go
type StrategyRiskConfig struct {
    // ... 既存
    PositionSizing *PositionSizingConfig `json:"position_sizing,omitempty"`

    // PR4: BookGate / EntryCooldown profile knobs.
    // 0 = inherit from env / disabled (matches RiskConfig zero-value semantics).
    MaxSlippageBps   float64 `json:"max_slippage_bps,omitempty"`
    MaxBookSidePct   float64 `json:"max_book_side_pct,omitempty"`
    EntryCooldownSec int     `json:"entry_cooldown_sec,omitempty"`
}
```

Validate 拡張：

```go
if p.Risk.MaxSlippageBps < 0 || p.Risk.MaxSlippageBps > 1000 {
    errs = append(errs, fmt.Errorf("strategy_risk.max_slippage_bps must be in [0, 1000] (got %v)", p.Risk.MaxSlippageBps))
}
if p.Risk.MaxBookSidePct < 0 || p.Risk.MaxBookSidePct > 100 {
    errs = append(errs, fmt.Errorf("strategy_risk.max_book_side_pct must be in [0, 100] (got %v)", p.Risk.MaxBookSidePct))
}
if p.Risk.EntryCooldownSec < 0 || p.Risk.EntryCooldownSec > 3600 {
    errs = append(errs, fmt.Errorf("strategy_risk.entry_cooldown_sec must be in [0, 3600] (got %v)", p.Risk.EntryCooldownSec))
}
```

**テスト**: 既存 `strategy_config_test.go` に：
- 正常範囲 (15 / 20 / 60)
- 境界値 (0 / 1000 / 100 / 3600)
- 範囲外 (-1 / 1001 / 101 / 3601) でエラー

**完了判定**: `go test ./internal/domain/entity/... -run StrategyRisk` 緑。

---

### Task 2: RiskManager.UpdateConfig の挙動を確認 + 部分マージヘルパー

**事前調査**: `usecase/risk.go:478` の `UpdateConfig` を読み、現状仕様を確認する。

**仮説 A** (丸ごと置換):

```go
func (rm *RiskManager) UpdateConfig(config entity.RiskConfig) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    rm.config = config
}
```

→ profile 反映用ヘルパー新設：

```go
// ApplyProfileRiskOverrides updates only fields that the profile explicitly
// sets to a non-zero value. Existing zero-handling semantics (env-var fallback)
// are preserved for fields the profile leaves unset.
func (rm *RiskManager) ApplyProfileRiskOverrides(slippageBps, bookSidePct float64, entryCooldownSec int) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    if slippageBps > 0 {
        rm.config.MaxSlippageBps = slippageBps
    }
    if bookSidePct > 0 {
        rm.config.MaxBookSidePct = bookSidePct
    }
    if entryCooldownSec > 0 {
        rm.config.EntryCooldownSec = entryCooldownSec
    }
}
```

**仮説 B** (既に部分マージ): `UpdateConfig` を流用するだけで済む可能性もある。Task 1 の前にコードを開いて確認する。

どちらにせよ MCP/API 経由の `UpdateConfig` は **後勝ち** なので、profile boot 後に MCP が古い値で上書きすることは **PR4 では受け入れる** (MCP の整合性は PR5 で別途扱う)。

**テスト**: `risk_test.go` に：
- ApplyProfileRiskOverrides で profile の 0 値が既存値を上書きしないこと
- 非 0 値はちゃんと上書きすること
- 既存 `UpdateConfig` の挙動が touch されていないこと

**完了判定**: `go test ./internal/usecase/... -run "ApplyProfile|UpdateConfig"` 緑。

---

### Task 3: main.go で profile を riskMgr に反映

**変更**: `cmd/main.go` の RiskManager 初期化直後に追加：

```go
riskMgr := usecase.NewRiskManager(entity.RiskConfig{...})

// ... orderExecutor / realtimeHub wiring ...

liveProfile := loadLiveProfile()

// PR4: profile-driven risk knobs (BookGate thresholds + entry cooldown).
// Applied AFTER NewRiskManager so they override the env-var defaults that
// the constructor used. Zero values in the profile leave env-var fallbacks
// untouched, so legacy deployments keep working.
if liveProfile != nil {
    riskMgr.ApplyProfileRiskOverrides(
        liveProfile.Risk.MaxSlippageBps,
        liveProfile.Risk.MaxBookSidePct,
        liveProfile.Risk.EntryCooldownSec,
    )
    slog.Info("event-pipeline: profile risk overrides applied",
        "profile", liveProfile.Name,
        "maxSlippageBps", liveProfile.Risk.MaxSlippageBps,
        "maxBookSidePct", liveProfile.Risk.MaxBookSidePct,
        "entryCooldownSec", liveProfile.Risk.EntryCooldownSec,
    )
}
```

注意点: `loadLiveProfile()` の現位置が `riskMgr` 初期化より後ろになっている → 再構成が必要。シンプルに「profile 読みを上に移動」する。`loadLiveProfile` には外部依存 (env var / file) しかなく副作用無いので safe.

**テスト**: 起動時挙動なので unit test より docker compose 検証で OK。logs に `profile risk overrides applied` が出ること、`/api/v1/status` の Config 部分が 3 フィールド入っていることを確認。

---

### Task 4: backtest path で profile → RiskConfig

**変更**:

- `cmd/backtest/main.go` の `buildRunInput`: profile.Risk から `MaxSlippageBps` / `MaxBookSidePct` / `EntryCooldownSec` を `RiskConfig` に積む
- `internal/interfaces/api/handler/backtest.go`: API 経由の backtest も同じくマップ

**テスト**: `runner_decision_log_test.go` 系で、profile に bps=15 を設定した backtest を 1 本流し、`Summary.BookGateRejects` に値が入ること（板薄時間帯があれば）or 配線が正しく通ったことを assert。

**完了判定**: backtest 1 本が新フィールド付きで通り、BookGate がアタッチされる。

---

### Task 5: production_ltc_60k.json の更新

**変更**: `backend/profiles/production_ltc_60k.json`

```json
"strategy_risk": {
    "stop_loss_percent": 14,
    "take_profit_percent": 4,
    ...
    "trailing_atr_multiplier": 2.5,
    "max_slippage_bps": 15,
    "max_book_side_pct": 20,
    "entry_cooldown_sec": 60,
    "position_sizing": { ... }
}
```

description フィールドにも PR4 適用済みである旨を 1 行追加（履歴トレース用）。

`production.json` / `production_eth.json` は **触らない** (運用していない or 別文脈)。

---

### Task 6: BookGate edge case test 拡充

設計書 §7.1 で「snapshot fresh/stale/missing × top-N 充足/不足 × 自分のサイズが top-N の M%」の matrix が必要とある。既存の `book_limit_test.go` は基本ケースは網羅。不足分があれば追加：

- snapshot が `nil` の場合（`AllowOnMissingBook=true` / `false` の両方）
- snapshot が空 (top-N が 0 行) の場合
- snapshot が古い (StaleAfterMillis 超過) の場合
- top-N 充足だが自分のサイズが top-N 累積の 50% の場合 (各 sidepct 設定値)
- VWAP が mid から大きく離れる ATR 急騰時の挙動

既存テストを読み、不足分のみ追加する方針で。

**完了判定**: `go test ./internal/usecase/booklimit/... -count=1` 緑、tests のカバレッジが体感的に 90%+。

---

### Task 7: backtest と live の対称性検証テスト

設計書 §7.3「同じ profile を読ませた時、両 runner で BookGate / cooldown 設定が同じ値で有効化される」を明示テスト：

- backtest runner と live event_pipeline の両方で profile を読み、`riskHandler.BookGate` が同じ Config (bps / sidepct / TopN) で構築されることを assert
- `riskMgr.config.EntryCooldownSec` が両方で同じ値になること

cmd 系のため真の unit test は難しい。`event_pipeline_test.go` / `runner_test.go` のレベルで profile-driven な配線を一気通貫テストする。

---

### Task 8: 全パッケージ緑 + 動作確認

```bash
go test ./... -race -count=1
go vet ./...
```

**動作確認**:

1. **profile 反映**: `docker compose up --build -d backend` → ログに `profile risk overrides applied` を確認
2. **BookGate 発火確認**: `/api/v1/status` の `Config.MaxSlippageBps=15` を確認、bot 動かして `decision_log` の `book_gate_outcome=VETOED` 行が出ることを観測
3. **EntryCooldown 発火確認**: 手動で position を作って close → 60 秒間 `decision_intent=COOLDOWN_BLOCKED` を観測
4. **30 分監視**: 異常な REJECTED 増加なし、panic なし、`pipelineRunning=true` 維持

---

## 4. テスト戦略

### 4.1 単体テスト

| 対象 | テスト内容 |
|---|---|
| `StrategyRiskConfig` Validate | 3 フィールド境界値 |
| `RiskManager.ApplyProfileRiskOverrides` | profile zero スキップ / 非 zero 上書き |
| BookGate edge cases | snapshot stale / empty / top-N 不足 / 自サイズ M% |
| Profile loading | profile JSON → entity.StrategyRiskConfig 正しくマップ |

### 4.2 統合テスト

- backtest 1 本 (production_ltc_60k profile + 短期間 + book replay 有り) で `Summary.BookGateRejects` が populated
- live と backtest の対称配線テスト

### 4.3 動作確認 (Docker)

- profile 値が live で反映されている (logs + API)
- BookGate / EntryCooldown が想定通り発火する

---

## 5. リスクと緩和

| リスク | 影響 | 緩和 |
|---|---|---|
| BookGate が想定以上に厳しく、ほぼ全 entry が VETOED される | 売買が止まる | 初期値を保守的に (bps=15, sidepct=20)、PDCA で sweep |
| EntryCooldown=60s で連続 entry を意図する局面で COOLDOWN_BLOCKED | 機会損失 | LTC 板薄なので 60s は妥当、PDCA で sweep |
| MCP/API の UpdateConfig が profile 値を上書きしてしまう | profile-driven な制御が効かない | PR4 では受け入れ、PR5 で MCP の挙動見直し |
| profile JSON の Validate ルール変更で既存 profile (60+) が break | 起動失敗 | Validate は 0 を許容、既存 profile はそのまま動く |
| BookGate 強制で、PR3 で「両建てバグ修正により出るようになった entry」が再び弾かれる | EXIT_CANDIDATE 流の意図が損なわれる | EXIT_CANDIDATE 自体は PR3 で skip 済みなので影響なし。Veto は新規 NEW_ENTRY のみが対象 |

---

## 6. PR 作成手順

1. ブランチ: `feat/bookgate-enable-profile`
2. コミット粒度（5〜6 コミット）：
   - **Commit 1**: StrategyRiskConfig に 3 フィールド + Validate
   - **Commit 2**: RiskManager.ApplyProfileRiskOverrides ヘルパー
   - **Commit 3**: main.go で profile → riskMgr 反映
   - **Commit 4**: backtest path (CLI + API) で profile → RiskConfig マージ
   - **Commit 5**: BookGate edge case test 拡充
   - **Commit 6**: production_ltc_60k.json 更新
3. PR 本文：「PR4 of 5」、動作変更（BookGate / EntryCooldown 発火）、保守的初期値の根拠、PDCA sweep への引き継ぎ
4. CI 緑で squash merge
5. マージ後、Docker 再起動 → 30 分監視

---

## 7. 完了の定義（DoD）

- [ ] 8 タスクすべて完了
- [ ] `go test ./... -race -count=1` 緑
- [ ] `go vet ./...` 警告なし
- [ ] profile JSON Validate の境界値テスト緑
- [ ] backtest と live の対称配線テスト緑
- [ ] BookGate edge case test 緑
- [ ] live 起動 → ログで profile risk overrides 適用確認
- [ ] `/api/v1/status` の Config に新フィールド値が見える
- [ ] LTC ポジション flat 確認 (`/positions` = `[]`)
- [ ] PR 本文に動作変更宣言 + 初期値の根拠

---

## 8. 後続 PR への引き継ぎ

PR4 マージ後、PR5 (UI 表示 + cleanup) の plan を書く。

PR5 のスコープ:

- frontend `/history?tab=decisions` で `signal_direction` / `decision_intent` / `book_gate_outcome` を表示
- "REJECTED (両建て総額)" だった行が "HOLD (保有中)" / "COOLDOWN_BLOCKED" / "VETOED" に変わるので UI 文言調整
- `entity.Signal{Action}` の使用箇所を deprecated コメント
- ドキュメント更新: AGENTS.md, docs/clean-architecture.md, docs/decision-log-health-check.md
- MCP / API の `UpdateConfig` 挙動の見直し (profile fields をどう保護するか)

PDCA sweep (entry_cooldown_sec / max_slippage_bps / max_book_side_pct):

- PR4 マージ後に PDCA で 3 軸 sweep を回す
- 結果を見て production_ltc_60k.json の値を最適化
- これは別 PR / 別作業 (本 plan §1.3 通り)
