import { createRootRoute, Link, Outlet } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/router-devtools'

export const Route = createRootRoute({
  component: () => (
    <>
      <div style={{ 
        padding: '1rem', 
        borderBottom: '1px solid #ddd',
        display: 'flex',
        gap: '1rem',
        alignItems: 'center',
        backgroundColor: '#f8f9fa'
      }}>
        <h2 style={{ margin: 0 }}>Rakuten Exchange</h2>
        <nav style={{ display: 'flex', gap: '1rem', marginLeft: 'auto' }}>
          <Link to="/" style={{ textDecoration: 'none', color: '#0066cc' }}>
            [Markets]
          </Link>
          <Link to="/orders" style={{ textDecoration: 'none', color: '#0066cc' }}>
            [Orders]
          </Link>
          <Link to="/account" style={{ textDecoration: 'none', color: '#0066cc' }}>
            [Account]
          </Link>
        </nav>
      </div>
      <div style={{ padding: '2rem' }}>
        <Outlet />
      </div>
      <TanStackRouterDevtools />
    </>
  ),
})
