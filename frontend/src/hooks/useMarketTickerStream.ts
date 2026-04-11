import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { buildRealtimeWebSocketUrl, type LiveTicker, type RealtimeEventMessage } from '../lib/api'

type ConnectionState = 'connecting' | 'connected' | 'disconnected'

export function useMarketTickerStream(symbolId: number) {
  const queryClient = useQueryClient()
  const [ticker, setTicker] = useState<LiveTicker | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting')
  const lastIndicatorInvalidateRef = useRef(0)

  useEffect(() => {
    // symbolId 変更時に旧シンボルの価格が残らないよう、即座にリセット
    setTicker(null)

    let active = true
    let socket: WebSocket | null = null
    let retryTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      setConnectionState('connecting')
      socket = new WebSocket(buildRealtimeWebSocketUrl(symbolId))

      socket.addEventListener('open', () => {
        if (!active) return
        setConnectionState('connected')
      })

      socket.addEventListener('message', (event) => {
        if (!active) return

        let payload: RealtimeEventMessage
        try {
          payload = JSON.parse(event.data) as RealtimeEventMessage
        } catch {
          return
        }

        switch (payload.type) {
          case 'ticker': {
            setTicker(payload.data)

            const now = Date.now()
            if (now - lastIndicatorInvalidateRef.current >= 30_000) {
              lastIndicatorInvalidateRef.current = now
              void queryClient.invalidateQueries({ queryKey: ['indicators', symbolId] })
              void queryClient.invalidateQueries({ queryKey: ['candles', symbolId] })
            }
            return
          }
          case 'status':
            void queryClient.invalidateQueries({ queryKey: ['status'] })
            void queryClient.invalidateQueries({ queryKey: ['pnl'] })
            return
          case 'config':
            void queryClient.invalidateQueries({ queryKey: ['config'] })
            void queryClient.invalidateQueries({ queryKey: ['status'] })
            void queryClient.invalidateQueries({ queryKey: ['pnl'] })
            return
          case 'market_trades':
            void queryClient.invalidateQueries({ queryKey: ['trades', symbolId] })
            return
          case 'orderbook':
            return
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
