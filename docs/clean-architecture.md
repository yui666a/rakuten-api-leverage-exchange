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
