// This file is auto-generated - do not edit manually
import { Route as rootRoute } from './routes/__root'
import { Route as IndexRoute } from './routes/index'

declare module '@tanstack/react-router' {
  interface FileRoutesByPath {
    '/': {
      preLoaderRoute: typeof IndexRoute
      parentRoute: typeof rootRoute
    }
  }
}

export const routeTree = rootRoute.addChildren([IndexRoute])
