# AGENTS.md

楽天ウォレット証拠金取引所 API を使った暗号資産自動売買システム（個人の技術研鑽目的）。

## Tech Stack

- Backend: Go 1.25 (Gin) / Clean Architecture / SQLite
- Frontend: TanStack Router + React 19 + Vite 7 / TypeScript / Tailwind CSS v4
- Infra: Docker Compose
- CI: GitHub Actions (`go test`, `pnpm test`)

## Runtime

- **Docker Compose で運用。** `docker compose` 経由でコンテナを操作すること。
- 起動: `docker compose up --build -d`
- 停止: `docker compose down`
- ログ: `docker compose logs -f backend`
- 再ビルド: `make restart`
- Backend: `localhost:38080` (コンテナ内 8080)
- Frontend: `localhost:33000` (コンテナ内 3000)
- API ベース URL: `http://localhost:38080/api/v1`

## Development

- コード変更後は `docker compose up --build -d` で再ビルドして動作確認。
- Backend テスト: `cd backend && go test ./... -race -count=1`
- Frontend テスト: `cd frontend && pnpm test`

## Conventions

- Go: 標準規約。`slog` でログ。テストは `_test.go`。
- Frontend: TypeScript strict。コンポーネントは `src/components/`、フックは `src/hooks/`。
- DB: SQLite。マイグレーションは `database/migrations.go`。
- Git: Conventional Commits (`feat:`, `fix:`, `refactor:`, `docs:`)。feature ブランチは `feat/xxx`。
- `.env` や API キーは絶対にコミットしない。

## Git Strategy

- GitHub Flow を採用する。
- `main` ブランチへの取り込みは Pull Request 経由で行う。
- PR の変更内容は一意にする（「ついでにリファクタ」禁止）。
- 1つの機能完成に複数PRが必要な場合、PRを Stack（数珠つなぎ）してよい。
- コミットは作業完了時の1コミットにまとめず、適切な粒度で分ける。

## Coding Strategy

- このプロダクトは個人開発であり、既存システム互換の維持を最優先にはしない。
- バグ/エラーを発見した場合、症状のみを抑える最小工数の応急処置ではなく、設計負債を残しにくい修正を優先する。
- 修正時は局所対処より、責務分離・依存関係・拡張性を含めた設計の整合性を重視する。

## Architecture

- Clean Architecture: domain → usecase → infrastructure → interfaces の依存方向を厳守。
- Trading Pipeline: 60秒間隔で指標計算 → Stance 判定 → シグナル → リスクチェック → 注文。
- Stance: `TREND_FOLLOW` / `CONTRARIAN` / `HOLD`。ルールベース自動判定 or オーバーライド。

## Docs (必要な時に読むこと)

| ドキュメント | 内容 | いつ読むか |
|---|---|---|
| `docs/project-structure.md` | ディレクトリ構成・全ファイル一覧 | ファイルの場所を探すとき |
| `docs/api-reference.md` | 全 API エンドポイント仕様 | API を叩くとき・ハンドラーを実装するとき |
| `docs/agent-operation-guide.md` | 自動売買の操作手順書 | 売買操作・Bot 制御・Stance 設定を行うとき |
| `docs/clean-architecture.md` | レイヤー構成と依存ルール | Backend のコードを追加・変更するとき |
| `docs/rakuten-api/error-codes.md` | 楽天 API エラーコード一覧と対処法 | API エラーのデバッグ・エラーハンドリング実装時 |
| `docs/design/` | 設計書・実装計画 | 各機能の設計意図を確認するとき |
| `backend/.env.example` | 環境変数テンプレート | 設定項目を確認するとき |
