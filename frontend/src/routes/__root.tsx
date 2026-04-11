import { HeadContent, Outlet, Scripts, createRootRoute } from '@tanstack/react-router'
import appCss from '../styles.css?url'
import { SymbolProvider } from '../contexts/SymbolContext'

type RootSearch = {
  symbol?: string
}

export const Route = createRootRoute({
  validateSearch: (search: Record<string, unknown>): RootSearch => ({
    symbol: typeof search.symbol === 'string' ? search.symbol : undefined,
  }),
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'Trading Bot Dashboard' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
    ],
  }),
  component: RootComponent,
})

function RootComponent() {
  return (
    <html lang="ja">
      <head>
        <HeadContent />
      </head>
      <body className="bg-bg-primary text-text-primary min-h-screen">
        <SymbolProvider>
          <Outlet />
        </SymbolProvider>
        <Scripts />
      </body>
    </html>
  )
}
