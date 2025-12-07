import { useState, useEffect } from 'react'
import { apiClient, Order } from '../lib/api'

export function OrderList() {
  const [orders, setOrders] = useState<Order[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadOrders()
  }, [])

  const loadOrders = async () => {
    try {
      setLoading(true)
      const data = await apiClient.getOrders()
      setOrders(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load orders')
    } finally {
      setLoading(false)
    }
  }

  const handleCancelOrder = async (id: string) => {
    if (!confirm('Are you sure you want to cancel this order?')) {
      return
    }

    try {
      await apiClient.cancelOrder(id)
      await loadOrders()
      alert('Order cancelled successfully')
    } catch (err) {
      alert(`Failed to cancel order: ${err instanceof Error ? err.message : 'Unknown error'}`)
    }
  }

  if (loading) {
    return <div style={{ padding: '1rem' }}>Loading orders...</div>
  }

  if (error) {
    return (
      <div style={{ padding: '1rem', color: '#dc3545' }}>
        Error: {error}
        <button onClick={loadOrders} style={{ marginLeft: '1rem' }}>Retry</button>
      </div>
    )
  }

  if (orders.length === 0) {
    return (
      <div style={{ 
        padding: '2rem', 
        textAlign: 'center', 
        color: '#666',
        backgroundColor: '#f8f9fa',
        borderRadius: '8px'
      }}>
        No orders yet. Go to Markets to create your first order.
      </div>
    )
  }

  return (
    <table style={{ 
      width: '100%', 
      borderCollapse: 'collapse' 
    }}>
      <thead>
        <tr style={{ backgroundColor: '#f8f9fa', borderBottom: '2px solid #dee2e6' }}>
          <th style={{ padding: '0.75rem', textAlign: 'left' }}>Order ID</th>
          <th style={{ padding: '0.75rem', textAlign: 'left' }}>Symbol</th>
          <th style={{ padding: '0.75rem', textAlign: 'center' }}>Side</th>
          <th style={{ padding: '0.75rem', textAlign: 'center' }}>Type</th>
          <th style={{ padding: '0.75rem', textAlign: 'right' }}>Price</th>
          <th style={{ padding: '0.75rem', textAlign: 'right' }}>Amount</th>
          <th style={{ padding: '0.75rem', textAlign: 'center' }}>Status</th>
          <th style={{ padding: '0.75rem', textAlign: 'center' }}>Action</th>
        </tr>
      </thead>
      <tbody>
        {orders.map((order) => (
          <tr key={order.id} style={{ borderBottom: '1px solid #dee2e6' }}>
            <td style={{ padding: '0.75rem', fontSize: '0.875rem', color: '#666' }}>
              {order.id?.substring(0, 8)}...
            </td>
            <td style={{ padding: '0.75rem', fontWeight: 'bold' }}>{order.symbol}</td>
            <td style={{ 
              padding: '0.75rem', 
              textAlign: 'center',
              color: order.side === 'buy' ? '#28a745' : '#dc3545',
              fontWeight: 'bold'
            }}>
              {order.side.toUpperCase()}
            </td>
            <td style={{ padding: '0.75rem', textAlign: 'center' }}>
              {order.type}
            </td>
            <td style={{ padding: '0.75rem', textAlign: 'right' }}>
              ¥{order.price.toLocaleString()}
            </td>
            <td style={{ padding: '0.75rem', textAlign: 'right' }}>
              {order.amount}
            </td>
            <td style={{ padding: '0.75rem', textAlign: 'center' }}>
              <span style={{
                padding: '0.25rem 0.75rem',
                borderRadius: '12px',
                backgroundColor: 
                  order.status === 'completed' ? '#d4edda' :
                  order.status === 'cancelled' ? '#f8d7da' :
                  '#fff3cd',
                color:
                  order.status === 'completed' ? '#155724' :
                  order.status === 'cancelled' ? '#721c24' :
                  '#856404',
                fontSize: '0.875rem'
              }}>
                {order.status}
              </span>
            </td>
            <td style={{ padding: '0.75rem', textAlign: 'center' }}>
              {order.status === 'pending' && (
                <button
                  onClick={() => order.id && handleCancelOrder(order.id)}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#dc3545',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: 'pointer',
                    fontSize: '0.875rem'
                  }}
                >
                  Cancel
                </button>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
