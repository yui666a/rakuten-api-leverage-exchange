import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { buildMarketWebSocketUrl, type LiveTicker, type MarketStreamMessage } from '../lib/api'

type ConnectionState = 'connecting' | 'connected' | 'disconnected'

export function useMarketTickerStream(symbolId: number) {
  const queryClient = useQueryClient()
  const [ticker, setTicker] = useState<LiveTicker | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting')
  const lastInvalidateRef = useRef(0)

  useEffect(() => {
    let active = true
    let socket: WebSocket | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      setConnectionState('connecting')
      socket = new WebSocket(buildMarketWebSocketUrl(symbolId))

      socket.addEventListener('open', () => {
        if (!active) return
        setConnectionState('connected')
      })

      socket.addEventListener('message', (event) => {
        if (!active) return

        let payload: MarketStreamMessage
        try {
          payload = JSON.parse(event.data) as MarketStreamMessage
        } catch {
          return
        }
        if (payload.type !== 'ticker') return

        setTicker(payload.data)

        const now = Date.now()
        if (now - lastInvalidateRef.current >= 30_000) {
          lastInvalidateRef.current = now
          void queryClient.invalidateQueries({ queryKey: ['indicators', symbolId] })
          void queryClient.invalidateQueries({ queryKey: ['candles', symbolId] })
        }
      })

      socket.addEventListener('close', () => {
        if (!active) return
        setConnectionState('disconnected')
        retryTimer = setTimeout(connect, 2_000)
      })

      socket.addEventListener('error', () => {
        socket?.close()
      })
    }

    connect()

    return () => {
      active = false
      if (retryTimer) clearTimeout(retryTimer)
      socket?.close()
    }
  }, [queryClient, symbolId])

  return {
    ticker,
    connectionState,
  }
}
