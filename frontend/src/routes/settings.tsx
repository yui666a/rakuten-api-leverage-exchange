import { createFileRoute, redirect } from '@tanstack/react-router'

// /settings は /operations に統合された。既存ブックマークやリンクを
// 壊さないため beforeLoad で恒久リダイレクトする。クエリ (symbol 等)
// はそのまま引き継ぐ。
export const Route = createFileRoute('/settings')({
  beforeLoad: ({ search }) => {
    throw redirect({ to: '/operations', search })
  },
})
