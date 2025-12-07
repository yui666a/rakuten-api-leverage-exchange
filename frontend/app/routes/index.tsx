import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/')({
  component: HomeComponent,
})

function HomeComponent() {
  return (
    <div style={{ padding: '20px' }}>
      <h1>Rakuten API Leverage Exchange</h1>
      <p>Welcome to your learning project!</p>
      <p>Backend: Gin (Golang) with Clean Architecture</p>
      <p>Frontend: TanStack Start (TypeScript)</p>
    </div>
  )
}
