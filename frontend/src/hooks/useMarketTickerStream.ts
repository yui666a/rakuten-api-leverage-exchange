import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  buildRealtimeWebSocketUrl,
  type LiveTicker,
  type RealtimeEventMessage,
  type RealtimeOrderbook,
} from '../lib/api'
import { useNotificationSettings } from './useNotificationSettings'
import { formatRiskEvent, formatTradeEvent } from '../lib/notify-format'
import { playBeep, showNotification } from '../lib/notifier'

type ConnectionState = 'connecting' | 'connected' | 'disconnected'

export function useMarketTickerStream(symbolId: number) {
  const queryClient = useQueryClient()
  const [ticker, setTicker] = useState<LiveTicker | null>(null)
  const [orderbook, setOrderbook] = useState<RealtimeOrderbook | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting')
  const lastIndicatorInvalidateRef = useRef(0)
  const { shouldFire, settings } = useNotificationSettings()
  // Read the latest values inside socket callbacks via refs so we don't have
  // to tear down + rebuild the WebSocket whenever the user toggles
  // notifications. The socket lifetime is tied to symbolId only.
  const shouldFireRef = useRef(shouldFire)
  const soundEnabledRef = useRef(settings.soundEnabled)
  shouldFireRef.current = shouldFire
  soundEnabledRef.current = settings.soundEnabled

  useEffect(() => {
    // symbolId 変更時に旧シンボルの価格が残らないよう、即座にリセット
    setTicker(null)
    setOrderbook(null)

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
            setOrderbook(payload.data)
            return
          case 'position_update':
            // RealExecutor pushes position changes immediately on diff
            // detection (see PR-O). Invalidate the positions query so the
            // dashboard panel refreshes without waiting for the periodic
            // sync interval.
            void queryClient.invalidateQueries({ queryKey: ['positions'] })
            return
          case 'trade_event': {
            // Open/close 約定: trades / positions / pnl の表示も最新化する。
            void queryClient.invalidateQueries({ queryKey: ['trades', symbolId] })
            void queryClient.invalidateQueries({ queryKey: ['positions'] })
            void queryClient.invalidateQueries({ queryKey: ['pnl'] })
            if (shouldFireRef.current) {
              const desc = formatTradeEvent(payload.data)
              showNotification(desc)
              if (soundEnabledRef.current) playBeep(desc.beep)
            }
            return
          }
          case 'risk_event': {
            void queryClient.invalidateQueries({ queryKey: ['status'] })
            if (shouldFireRef.current) {
              const desc = formatRiskEvent(payload.data)
              showNotification(desc)
              if (soundEnabledRef.current) playBeep(desc.beep)
            }
            return
          }
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
    orderbook,
    connectionState,
  }
}
