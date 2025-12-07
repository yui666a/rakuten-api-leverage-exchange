import { createFileRoute } from '@tanstack/react-router'
import { OrderList } from '../components/OrderList'

export const Route = createFileRoute('/orders')({
  component: Orders,
})

function Orders() {
  return (
    <div>
      <h1>Orders</h1>
      <p style={{ color: '#666', marginBottom: '2rem' }}>
        Manage your trading orders
      </p>
      <OrderList />
    </div>
  )
}
