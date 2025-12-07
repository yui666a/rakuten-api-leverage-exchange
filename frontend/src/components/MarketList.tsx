import { useState, useEffect } from 'react'
import { apiClient, Market } from '../lib/api'
import { CreateOrderForm } from './CreateOrderForm'

export function MarketList() {
  const [markets, setMarkets] = useState<Market[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedMarket, setSelectedMarket] = useState<Market | null>(null)

  useEffect(() => {
    loadMarkets()
  }, [])

  const loadMarkets = async () => {
    try {
      setLoading(true)
      const data = await apiClient.getMarkets()
      setMarkets(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load markets')
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return <div style={{ padding: '1rem' }}>Loading markets...</div>
  }

  if (error) {
    return (
      <div style={{ padding: '1rem', color: '#dc3545' }}>
        Error: {error}
        <button onClick={loadMarkets} style={{ marginLeft: '1rem' }}>Retry</button>
      </div>
    )
  }

  return (
    <div>
      <table style={{ 
        width: '100%', 
        borderCollapse: 'collapse',
        marginBottom: '2rem'
      }}>
        <thead>
          <tr style={{ backgroundColor: '#f8f9fa', borderBottom: '2px solid #dee2e6' }}>
            <th style={{ padding: '0.75rem', textAlign: 'left' }}>Symbol</th>
            <th style={{ padding: '0.75rem', textAlign: 'right' }}>Last Price</th>
            <th style={{ padding: '0.75rem', textAlign: 'right' }}>24h Change</th>
            <th style={{ padding: '0.75rem', textAlign: 'right' }}>Volume</th>
            <th style={{ padding: '0.75rem', textAlign: 'right' }}>High</th>
            <th style={{ padding: '0.75rem', textAlign: 'right' }}>Low</th>
            <th style={{ padding: '0.75rem', textAlign: 'center' }}>Action</th>
          </tr>
        </thead>
        <tbody>
          {markets.map((market) => (
            <tr key={market.symbol} style={{ borderBottom: '1px solid #dee2e6' }}>
              <td style={{ padding: '0.75rem', fontWeight: 'bold' }}>{market.symbol}</td>
              <td style={{ padding: '0.75rem', textAlign: 'right' }}>
                ¥{market.last_price.toLocaleString()}
              </td>
              <td style={{ 
                padding: '0.75rem', 
                textAlign: 'right',
                color: market.change_24h >= 0 ? '#28a745' : '#dc3545'
              }}>
                {market.change_24h >= 0 ? '+' : ''}{market.change_24h.toFixed(2)}%
              </td>
              <td style={{ padding: '0.75rem', textAlign: 'right' }}>
                {market.volume.toLocaleString()}
              </td>
              <td style={{ padding: '0.75rem', textAlign: 'right' }}>
                ¥{market.high_24h.toLocaleString()}
              </td>
              <td style={{ padding: '0.75rem', textAlign: 'right' }}>
                ¥{market.low_24h.toLocaleString()}
              </td>
              <td style={{ padding: '0.75rem', textAlign: 'center' }}>
                <button 
                  onClick={() => setSelectedMarket(market)}
                  style={{
                    padding: '0.5rem 1rem',
                    backgroundColor: '#0066cc',
                    color: 'white',
                    border: 'none',
                    borderRadius: '4px',
                    cursor: 'pointer'
                  }}
                >
                  Trade
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {selectedMarket && (
        <div style={{
          position: 'fixed',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(0,0,0,0.5)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          zIndex: 1000
        }}>
          <div style={{
            backgroundColor: 'white',
            padding: '2rem',
            borderRadius: '8px',
            maxWidth: '500px',
            width: '90%'
          }}>
            <h2>Trade {selectedMarket.symbol}</h2>
            <CreateOrderForm 
              symbol={selectedMarket.symbol} 
              onClose={() => setSelectedMarket(null)}
            />
          </div>
        </div>
      )}
    </div>
  )
}
