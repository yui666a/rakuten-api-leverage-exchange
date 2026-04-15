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

- ブランチ運用は GitHub Flow を採用する。
- `main` への変更取り込みは、必ず Pull Request（PR）経由で行う。
- 1つの PR は 1つの目的に限定する。無関係な変更（「ついでのリファクタ」「別機能の修正」）を混在させない。
- 機能実装が大きく、1PR で完結しない場合は Stacked PR（数珠つなぎの PR）を許容する。
- Stacked PR でも各 PR の目的は独立して説明可能な状態にし、レビュー可能な粒度を保つ。
- コミットは「作業完了後に1コミットへ圧縮」ではなく、レビューと追跡に適した粒度で分割する。

## Coding Strategy

- 本プロダクトは個人開発であり、既存実装との互換維持だけを最優先にはしない。
- バグやエラーを修正する際は、症状を一時的に抑えるだけの応急処置ではなく、再発しにくく設計負債を増やしにくい解決を優先する。
- 修正方針は「最小差分」より「設計の整合性」を優先し、責務分離・依存方向・拡張性を崩さない実装を選ぶ。
- 必要であれば関連箇所を含めて構造的に直し、同種の問題が横展開しない状態を目指す。

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
