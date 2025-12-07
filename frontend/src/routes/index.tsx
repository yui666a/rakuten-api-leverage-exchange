import { createFileRoute } from '@tanstack/react-router'
import { MarketList } from '../components/MarketList'

export const Route = createFileRoute('/')({
  component: Index,
})

function Index() {
  return (
    <div>
      <h1>Markets</h1>
      <p style={{ color: '#666', marginBottom: '2rem' }}>
        View real-time market data and trading pairs
      </p>
      <MarketList />
    </div>
  )
}
