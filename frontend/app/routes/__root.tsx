import { createRootRoute, Outlet } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: RootComponent,
})

function RootComponent() {
  return (
    <html lang="ja">
      <head>
        <meta charSet="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>Rakuten API Leverage Exchange</title>
      </head>
      <body>
        <div id="app">
          <Outlet />
        </div>
      </body>
    </html>
  )
}
