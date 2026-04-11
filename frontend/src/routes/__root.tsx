import { HeadContent, Outlet, Scripts, createRootRoute } from '@tanstack/react-router'
import appCss from '../styles.css?url'
import { SymbolProvider } from '../contexts/SymbolContext'

export const Route = createRootRoute({
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
