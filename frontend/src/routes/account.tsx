import { createFileRoute } from '@tanstack/react-router'
import { AccountInfo } from '../components/AccountInfo'

export const Route = createFileRoute('/account')({
  component: Account,
})

function Account() {
  return (
    <div>
      <h1>Account</h1>
      <p style={{ color: '#666', marginBottom: '2rem' }}>
        View your account balance and information
      </p>
      <AccountInfo />
    </div>
  )
}
