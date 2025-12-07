import { useState } from 'react'
import { apiClient, Order } from '../lib/api'

interface CreateOrderFormProps {
  symbol: string
  onClose: () => void
}

export function CreateOrderForm({ symbol, onClose }: CreateOrderFormProps) {
  const [side, setSide] = useState<'buy' | 'sell'>('buy')
  const [type, setType] = useState<'market' | 'limit'>('limit')
  const [price, setPrice] = useState('')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setError(null)

    try {
      const order: Order = {
        symbol,
        side,
        type,
        price: parseFloat(price),
        amount: parseFloat(amount),
      }

      await apiClient.createOrder(order)
      alert('Order created successfully!')
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create order')
    } finally {
      setLoading(false)
    }
  }

  return (
    <form onSubmit={handleSubmit}>
      <div style={{ marginBottom: '1rem' }}>
        <label style={{ display: 'block', marginBottom: '0.5rem' }}>
          Side
        </label>
        <select 
          value={side} 
          onChange={(e) => setSide(e.target.value as 'buy' | 'sell')}
          style={{ width: '100%', padding: '0.5rem', fontSize: '1rem' }}
        >
          <option value="buy">Buy</option>
          <option value="sell">Sell</option>
        </select>
      </div>

      <div style={{ marginBottom: '1rem' }}>
        <label style={{ display: 'block', marginBottom: '0.5rem' }}>
          Type
        </label>
        <select 
          value={type} 
          onChange={(e) => setType(e.target.value as 'market' | 'limit')}
          style={{ width: '100%', padding: '0.5rem', fontSize: '1rem' }}
        >
          <option value="limit">Limit</option>
          <option value="market">Market</option>
        </select>
      </div>

      <div style={{ marginBottom: '1rem' }}>
        <label style={{ display: 'block', marginBottom: '0.5rem' }}>
          Price (¥)
        </label>
        <input
          type="number"
          value={price}
          onChange={(e) => setPrice(e.target.value)}
          required
          step="0.01"
          min="0"
          style={{ width: '100%', padding: '0.5rem', fontSize: '1rem' }}
        />
      </div>

      <div style={{ marginBottom: '1rem' }}>
        <label style={{ display: 'block', marginBottom: '0.5rem' }}>
          Amount
        </label>
        <input
          type="number"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          required
          step="0.001"
          min="0"
          style={{ width: '100%', padding: '0.5rem', fontSize: '1rem' }}
        />
      </div>

      {error && (
        <div style={{ color: '#dc3545', marginBottom: '1rem' }}>
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: '1rem' }}>
        <button
          type="submit"
          disabled={loading}
          style={{
            flex: 1,
            padding: '0.75rem',
            backgroundColor: side === 'buy' ? '#28a745' : '#dc3545',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: loading ? 'not-allowed' : 'pointer',
            fontSize: '1rem',
            fontWeight: 'bold'
          }}
        >
          {loading ? 'Submitting...' : `${side.toUpperCase()} ${symbol}`}
        </button>
        <button
          type="button"
          onClick={onClose}
          style={{
            padding: '0.75rem 1.5rem',
            backgroundColor: '#6c757d',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
            fontSize: '1rem'
          }}
        >
          Cancel
        </button>
      </div>
    </form>
  )
}
