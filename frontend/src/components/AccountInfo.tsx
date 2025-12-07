import { useState, useEffect } from 'react'
import { apiClient, Account } from '../lib/api'

export function AccountInfo() {
  const [account, setAccount] = useState<Account | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadAccount()
  }, [])

  const loadAccount = async () => {
    try {
      setLoading(true)
      // Using "default" as the account ID for demonstration
      const data = await apiClient.getAccount('default')
      setAccount(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load account')
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return <div style={{ padding: '1rem' }}>Loading account...</div>
  }

  if (error) {
    return (
      <div style={{ padding: '1rem', color: '#dc3545' }}>
        Error: {error}
        <button onClick={loadAccount} style={{ marginLeft: '1rem' }}>Retry</button>
      </div>
    )
  }

  if (!account) {
    return <div style={{ padding: '1rem' }}>No account data available</div>
  }

  const availableBalance = account.balance - account.locked_funds

  return (
    <div style={{ 
      maxWidth: '600px',
      margin: '0 auto'
    }}>
      <div style={{
        backgroundColor: '#f8f9fa',
        padding: '2rem',
        borderRadius: '8px',
        border: '1px solid #dee2e6'
      }}>
        <div style={{ marginBottom: '1.5rem' }}>
          <div style={{ 
            fontSize: '0.875rem', 
            color: '#666',
            marginBottom: '0.5rem'
          }}>
            Account ID
          </div>
          <div style={{ fontSize: '1.125rem', fontWeight: 'bold' }}>
            {account.id}
          </div>
        </div>

        <div style={{ marginBottom: '1.5rem' }}>
          <div style={{ 
            fontSize: '0.875rem', 
            color: '#666',
            marginBottom: '0.5rem'
          }}>
            Total Balance
          </div>
          <div style={{ fontSize: '2rem', fontWeight: 'bold', color: '#28a745' }}>
            ¥{account.balance.toLocaleString()} {account.currency}
          </div>
        </div>

        <div style={{ 
          display: 'grid', 
          gridTemplateColumns: '1fr 1fr', 
          gap: '1rem',
          paddingTop: '1.5rem',
          borderTop: '1px solid #dee2e6'
        }}>
          <div>
            <div style={{ 
              fontSize: '0.875rem', 
              color: '#666',
              marginBottom: '0.5rem'
            }}>
              Available Balance
            </div>
            <div style={{ fontSize: '1.25rem', fontWeight: 'bold' }}>
              ¥{availableBalance.toLocaleString()}
            </div>
          </div>

          <div>
            <div style={{ 
              fontSize: '0.875rem', 
              color: '#666',
              marginBottom: '0.5rem'
            }}>
              Locked Funds
            </div>
            <div style={{ fontSize: '1.25rem', fontWeight: 'bold', color: '#dc3545' }}>
              ¥{account.locked_funds.toLocaleString()}
            </div>
          </div>
        </div>

        <div style={{ 
          marginTop: '1.5rem',
          paddingTop: '1.5rem',
          borderTop: '1px solid #dee2e6',
          fontSize: '0.875rem',
          color: '#666'
        }}>
          Last updated: {new Date(account.updated_at).toLocaleString()}
        </div>
      </div>
    </div>
  )
}
