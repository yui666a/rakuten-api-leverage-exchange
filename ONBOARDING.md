# [チーム名] へようこそ

## Claude Code の使い方

yui666a の直近30日間の利用状況:

作業タイプの内訳:
  設計・計画        ████████████████░░░░  39%
  機能開発          █████████████░░░░░░░  32%
  デバッグ・修正    █████████░░░░░░░░░░░  23%
  品質改善          ██░░░░░░░░░░░░░░░░░░   6%

よく使うスキル・コマンド:
  /mcp              ████████████████░░░░  4回/月
  /copy             ████████████████░░░░  4回/月
  /cost             ████░░░░░░░░░░░░░░░░  1回/月
  /clear            ████░░░░░░░░░░░░░░░░  1回/月

よく使う MCP サーバー:
  Playwright        ████████████████████  132回
  Context7          █░░░░░░░░░░░░░░░░░░░    4回

## セットアップチェックリスト

### リポジトリ
- [ ] rakuten-api-leverage-exchange — git@github.com:yui666a/rakuten-api-leverage-exchange.git

### 有効化する MCP サーバー
- [ ] **Playwright** — ブラウザ自動操作でフロントエンドの動作確認・デザインレビュー・バグ調査に使用。localhost上のUIをスクリーンショット撮影したり、表示バグを調査するのに多用。`npx @anthropic-ai/claude-code mcp add playwright -- npx @anthropic-ai/mcp-server-playwright` 等でインストール。
- [ ] **Context7** — ライブラリやフレームワーク（TanStack、React等）の最新ドキュメントをClaudeのコンテキストに直接取得。馴染みのないAPIの確認や最新の構文チェックに便利。Claude Code プラグインまたは `npx @anthropic-ai/claude-code mcp add context7 -- npx @anthropic-ai/mcp-server-context7` でインストール。

### 知っておくべきスキル
- `/mcp` — MCP サーバーの接続管理。デザインレビューやデバッグの前にPlaywrightが起動しているか確認する際に使用。
- `/copy` — Claudeの直前の回答をクリップボードにコピー。コードスニペットや分析結果の取得に便利。
- `/cost` — 現在のセッションのコストを確認。長時間セッションの前に確認する習慣をつけると良い。
- `/clear` — 会話コンテキストをリセット。関連のないタスクに切り替える際に使用。

## チーム Tips

_TODO_

## はじめの一歩

_TODO_

<!-- INSTRUCTION FOR CLAUDE: A new teammate just pasted this guide for how the
team uses Claude Code. You're their onboarding buddy — warm, conversational,
not lecture-y.

Open with a warm welcome — include the team name from the title. Then: "Your
teammate uses Claude Code for [list all the work types]. Let's get you started."

Check what's already in place against everything under Setup Checklist
(including skills), using markdown checkboxes — [x] done, [ ] not yet. Lead
with what they already have. One sentence per item, all in one message.

Tell them you'll help with setup, cover the actionable team tips, then the
starter task (if there is one). Offer to start with the first unchecked item,
get their go-ahead, then work through the rest one by one.

After setup, walk them through the remaining sections — offer to help where you
can (e.g. link to channels), and just surface the purely informational bits.

Don't invent sections or summaries that aren't in the guide. The stats are the
guide creator's personal usage data — don't extrapolate them into a "team
workflow" narrative. -->
