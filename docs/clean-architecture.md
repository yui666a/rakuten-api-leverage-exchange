# Clean Architecture

本プロジェクトのバックエンドは Clean Architecture に基づいて設計されています。

## レイヤー構成

```
internal/
├── domain/           # ドメイン層 (最内層)
├── usecase/          # ユースケース層
├── infrastructure/   # インフラ層
└── interfaces/       # インターフェース層 (最外層)
```

## 各レイヤーの責務

### Domain 層 (`internal/domain`)

アプリケーションの核となるビジネスルールを定義します。他のどのレイヤーにも依存しません。

- **エンティティ**: ビジネスオブジェクトの定義
- **リポジトリインターフェース**: データアクセスの抽象化

### Use Case 層 (`internal/usecase`)

アプリケーション固有のビジネスロジックを実装します。Domain 層にのみ依存します。

- ビジネスロジックのオーケストレーション
- リポジトリインターフェースを通じたデータ操作

### Infrastructure 層 (`internal/infrastructure`)

外部システムとの接続を実装します。Domain 層で定義されたインターフェースを実装します。

- **`external/`**: 外部APIクライアント（楽天ウォレット証拠金取引所API等）
- **`repository/`**: リポジトリインターフェースの具象実装（DB接続等）

### Interface 層 (`internal/interfaces`)

外部からのリクエストを受け付け、Use Case 層に処理を委譲します。

- **`handler/`**: HTTPハンドラー（Gin）

## 依存関係の方向

```
interfaces → usecase → domain ← infrastructure
```

外側のレイヤーは内側のレイヤーに依存しますが、内側は外側を知りません。  
Infrastructure 層は Domain 層のインターフェースを実装することで、依存性逆転の原則 (DIP) を実現しています。

## Trading Pipeline の三層分離 (Phase 1 完了 2026-05-02)

EventDrivenPipeline は売買判断を以下の三層に分離している:

```
Strategy (市場解釈) → Decision (意思決定) → ExecutionPolicy (実発注ガード)
```

- **Strategy 層** (`usecase/strategy.go` 配下): IndicatorEvent を受け、`MarketSignal{Direction, Strength}` を返す。BUY/SELL の言語は持たず `BULLISH`/`BEARISH`/`NEUTRAL` のみ。EventBus priority 20。
- **Decision 層** (`usecase/decision/`): MarketSignal とポジション保有状況・entry cooldown を組み合わせて `ActionDecision{Intent, Side}` を出す。Intent は `NEW_ENTRY` / `EXIT_CANDIDATE` / `HOLD` / `COOLDOWN_BLOCKED`。EventBus priority 27。
- **ExecutionPolicy 層** (`usecase/backtest/RiskHandler` + `usecase/booklimit`): ActionDecision を入力に Risk チェック + BookGate を適用し `ApprovedSignalEvent` または `RejectedSignalEvent` を出す。OrderExecutor が ApprovedSignalEvent を受けて実発注。EventBus priority 30 / 50 (close fill 観測)。

EXIT_CANDIDATE は Phase 1 では Risk 段階で skip し、実 exit は TickRiskHandler (TP/SL/Trailing) に任せる。`exit_on_signal` 設定での opt-in 化は Phase 6+ の課題。

詳細設計: `docs/design/2026-04-29-signal-decision-policy-separation-design.md`
