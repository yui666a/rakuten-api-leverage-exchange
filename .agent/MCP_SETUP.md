# MCP (Model Context Protocol) セットアップガイド

このドキュメントでは、このプロジェクトに設定された MCP サーバーとそのセットアップ方法について説明します。

## 概要

このプロジェクトでは、HTTP トランスポートを優先し、ローカルインストールを最小限に抑えた、必須の開発ツールに焦点を当てた**最小限の MCP サーバー構成**を使用しています。

## 現在の MCP サーバー構成

### 1. 🌐 GitHub Copilot MCP (HTTP)

**設定:**
```json
{
  "type": "http",
  "url": "https://api.githubcopilot.com/mcp/"
}
```

**詳細:**
- **トランスポート**: HTTP（ローカルインストール不要）
- **用途**: GitHub リポジトリ操作、Issue 管理、PR 操作
- **必要条件**: GitHub Copilot へのアクセス
- **ディスク使用量**: 0 MB

**機能:**
- リポジトリとコードの検索
- Issue の作成と管理
- プルリクエストの作成と管理
- リポジトリ情報へのアクセス

---

### 2. 🎭 Playwright (npx)

**設定:**
```json
{
  "command": "npx",
  "args": ["@playwright/mcp@latest"]
}
```

**詳細:**
- **トランスポート**: stdio (npx)
- **用途**: E2E テストとブラウザ自動化
- **使用例**: TanStack Start フロントエンドのテスト
- **ディスク使用量**: 約 10-15 MB（npx キャッシュ）

**機能:**
- ブラウザ自動化
- E2E テスト
- スクリーンショットとビデオキャプチャ
- ネットワークインターセプト

---

### 3. 🔧 Chrome DevTools (npx)

**設定:**
```json
{
  "command": "npx",
  "args": ["-y", "chrome-devtools-mcp@latest"]
}
```

**詳細:**
- **トランスポート**: stdio (npx)
- **用途**: ブラウザデバッグと DOM 操作
- **使用例**: フロントエンド開発時のデバッグ
- **ディスク使用量**: 約 5 MB（npx キャッシュ）

**機能:**
- DOM インスペクション
- コンソールアクセス
- ネットワーク監視
- パフォーマンスプロファイリング

---

### 4. 🤖 Serena (uvx)

**設定:**
```json
{
  "type": "stdio",
  "command": "uvx",
  "args": [
    "--from", "git+https://github.com/oraios/serena",
    "serena", "start-mcp-server",
    "--context", "ide-assistant",
    "--project", "{PATH_TO_PROJECT}/rakuten-api-leverage-exchange"
  ]
}
```

**詳細:**
- **トランスポート**: stdio (uvx)
- **用途**: AI 搭載のコード分析とリファクタリング
- **必要条件**: Python と uvx
- **ディスク使用量**: 約 20-30 MB（Python パッケージ）

**機能:**
- コード分析
- リファクタリング提案
- コード品質インサイト
- 多言語サポート（Go、TypeScript）

---

## セットアップ手順

### 前提条件

1. **Node.js と npm** - npx ベースのサーバー用
2. **GitHub Copilot へのアクセス** - GitHub Copilot MCP 用
3. **Python と uvx** - Serena 用

### ステップ 1: uvx のインストール（Serena 用）

```bash
# Homebrew を使用（macOS 推奨）
brew install uv

# または pip を使用
pip install uv

# インストールの確認
which uvx
```

### ステップ 2: GitHub Copilot アクセスの確認

有効な GitHub Copilot サブスクリプションがあり、認証されていることを確認してください。

### ステップ 3: 開発環境の再起動

設定が完了したら、IDE を再起動して MCP サーバーを読み込みます。

---

## リソース使用量サマリー

| サーバー | タイプ | ディスク使用量 | メモリ（アクティブ時） |
|--------|------|------------|-----------------|
| GitHub Copilot MCP | HTTP | 0 MB | 最小限 |
| Playwright | npx | 約 10-15 MB | 約 50-100 MB |
| Chrome DevTools | npx | 約 5 MB | 約 30-50 MB |
| Serena | uvx | 約 20-30 MB | 約 100-200 MB |
| **合計** | | **約 35-50 MB** | **約 180-350 MB** |

**注意:**
- ディスク使用量はキャッシュされたパッケージの分
- メモリ使用量はサーバーがアクティブに使用されている時のみ
- npx サーバーは初回使用時にキャッシュ、以降の実行では追加ダウンロード不要

---

## このプロジェクトでの使用例

あなたの技術スタック（Gin/Golang バックエンド + TanStack Start/TypeScript フロントエンド）での活用:

### 1. GitHub Copilot MCP
- バグや機能の Issue を作成
- プルリクエストの管理
- リポジトリ全体でコード検索

### 2. Playwright
- TanStack Start フロントエンドの E2E テスト
- 自動ブラウザテスト
- ビジュアルリグレッションテスト

### 3. Chrome DevTools
- TanStack Start の React コンポーネントのデバッグ
- DOM と CSS のインスペクション
- フロントエンドから Gin バックエンドへのネットワークリクエストの監視

### 4. Serena
- バックエンドの Go コード分析（Clean Architecture）
- フロントエンドの TypeScript コードのリファクタリング
- 両方の言語のコード品質推奨事項

---

## トラブルシューティング

### Serena が起動しない

**エラー:** `uvx: command not found`

**解決策:**
```bash
# uv をインストール
brew install uv
# または
pip install uv
```

**エラー:** Serena の初期化に失敗

**解決策:**
```bash
# Python のインストールを確認
python3 --version

# Serena を手動で実行してみる
uvx --from git+https://github.com/oraios/serena serena --help
```

### Playwright/Chrome DevTools が動作しない

**問題:** 初回実行に時間がかかる

**説明:** npx は初回使用時にパッケージをダウンロードします。以降の実行は即座に行われます。

**問題:** ダウンロード中のネットワークエラー

**解決策:** インターネット接続を確認し、再試行してください。npx は正常にダウンロードされたパッケージをキャッシュします。

### GitHub Copilot MCP 接続の問題

**症状:** GitHub Copilot MCP に接続できない

**チェックリスト:**
1. GitHub Copilot サブスクリプションがアクティブか確認
2. GitHub で認証されているか確認
3. IDE を再起動
4. IDE の MCP サーバーステータス/ログを確認

---

## サーバーの追加

追加の MCP サーバーを追加するには、`.agent/mcp.json` を編集して新しいサーバー構成を追加します。

**例 - Git サーバーの追加:**
```json
{
  "mcpServers": {
    "git": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-git",
        "--repository",
        "/Users/h.aiso/Project/private/rakuten-api-leverage-exchange"
      ]
    }
  }
}
```

---

## 追加リソース

- [MCP 仕様](https://modelcontextprotocol.io/)
- [公式 MCP サーバー](https://github.com/modelcontextprotocol/servers)
- [Playwright MCP ドキュメント](https://playwright.dev/)
- [Serena GitHub リポジトリ](https://github.com/oraios/serena)

---

## 次のステップ

1. ✅ uvx をインストール: `brew install uv`
2. ✅ IDE を再起動
3. ✅ GitHub Copilot MCP 接続を確認
4. ✅ フロントエンド E2E テスト用に Playwright をテスト
5. ⏭️ コード分析とリファクタリングのために Serena を探索
