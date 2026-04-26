import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNotificationSettings, type BrowserPermission } from '../hooks/useNotificationSettings'

// Bell-with-popover that lives in the header. The popover is rendered in a
// portal anchored to <body> so it isn't clipped by the header's overflow-hidden
// (needed for the rounded gradient background).

const POPOVER_WIDTH = 288 // matches w-72 (18rem)
const POPOVER_GAP = 8 // px gap between bell and popover

export function NotificationToggle() {
  const { settings, permission, setEnabled, setSoundEnabled, requestPermission } =
    useNotificationSettings()
  const [open, setOpen] = useState(false)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)

  useLayoutEffect(() => {
    if (!open) return
    const update = () => {
      const btn = buttonRef.current
      if (!btn) return
      const r = btn.getBoundingClientRect()
      const left = Math.max(8, Math.min(window.innerWidth - POPOVER_WIDTH - 8, r.right - POPOVER_WIDTH))
      const top = r.bottom + POPOVER_GAP
      setPos({ top, left })
    }
    update()
    window.addEventListener('resize', update)
    window.addEventListener('scroll', update, true)
    return () => {
      window.removeEventListener('resize', update)
      window.removeEventListener('scroll', update, true)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      const target = e.target as Node
      if (buttonRef.current?.contains(target)) return
      if (popoverRef.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onClick)
    return () => document.removeEventListener('mousedown', onClick)
  }, [open])

  const isOn = settings.enabled && permission === 'granted'
  const ariaLabel = isOn ? '通知設定 (オン)' : '通知設定 (オフ)'

  return (
    <div className="relative">
      <button
        ref={buttonRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label={ariaLabel}
        className={`flex h-9 w-9 items-center justify-center rounded-full border transition ${
          isOn
            ? 'border-accent-green/50 bg-accent-green/15 text-accent-green'
            : 'border-white/15 bg-white/5 text-slate-300 hover:bg-white/10'
        }`}
      >
        <BellIcon active={isOn} />
      </button>
      {open && pos && typeof document !== 'undefined' && createPortal(
        <div
          ref={popoverRef}
          style={{ position: 'fixed', top: pos.top, left: pos.left, width: POPOVER_WIDTH }}
          className="z-50 rounded-2xl border border-white/10 bg-bg-card/95 p-4 shadow-[0_20px_60px_rgba(0,0,0,0.5)] backdrop-blur"
        >
          <p className="text-xs uppercase tracking-[0.28em] text-text-secondary">通知設定</p>
          <h3 className="mt-1 text-base font-semibold text-white">取引イベント通知</h3>

          <ToggleRow
            label="通知を有効化"
            checked={settings.enabled}
            onChange={(v) => void setEnabled(v)}
          />
          <ToggleRow
            label="効果音を鳴らす"
            checked={settings.soundEnabled}
            onChange={setSoundEnabled}
            disabled={!settings.enabled}
          />

          <div className="mt-3 rounded-xl border border-white/8 bg-white/4 px-3 py-2 text-xs text-slate-300">
            ブラウザ権限: <PermissionBadge value={permission} />
            {permission === 'default' && settings.enabled && (
              <button
                type="button"
                onClick={() => void requestPermission()}
                className="ml-2 rounded-full bg-accent-green/20 px-2 py-0.5 text-[10px] font-semibold text-accent-green hover:bg-accent-green/30"
              >
                許可をリクエスト
              </button>
            )}
            {permission === 'denied' && settings.enabled && (
              <p className="mt-1 text-[11px] text-accent-red">
                ブラウザ設定で通知をブロックしています。サイト設定から許可に変更してください。
              </p>
            )}
            {permission === 'unsupported' && (
              <p className="mt-1 text-[11px] text-slate-400">このブラウザは通知をサポートしていません</p>
            )}
          </div>

          <p className="mt-3 text-[11px] leading-relaxed text-text-secondary">
            エントリー / クローズ / リスク警告 (DD・連敗・日次損失) を OS 通知で表示します。
          </p>
        </div>,
        document.body,
      )}
    </div>
  )
}

function ToggleRow({
  label,
  checked,
  onChange,
  disabled,
}: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
}) {
  return (
    <label
      className={`mt-3 flex items-center justify-between text-sm ${
        disabled ? 'opacity-50' : 'text-slate-200'
      }`}
    >
      <span>{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => !disabled && onChange(!checked)}
        disabled={disabled}
        className={`relative inline-flex h-5 w-9 items-center rounded-full transition ${
          checked ? 'bg-accent-green' : 'bg-white/15'
        }`}
      >
        <span
          className={`inline-block h-4 w-4 transform rounded-full bg-white transition ${
            checked ? 'translate-x-4' : 'translate-x-1'
          }`}
        />
      </button>
    </label>
  )
}

function PermissionBadge({ value }: { value: BrowserPermission }) {
  const map: Record<BrowserPermission, { text: string; cls: string }> = {
    granted: { text: 'granted', cls: 'text-accent-green' },
    denied: { text: 'denied', cls: 'text-accent-red' },
    default: { text: 'pending', cls: 'text-slate-300' },
    unsupported: { text: 'unsupported', cls: 'text-slate-400' },
  }
  const v = map[value]
  return <span className={`font-mono ${v.cls}`}>{v.text}</span>
}

function BellIcon({ active }: { active: boolean }) {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
      {active && <circle cx="18" cy="6" r="2.5" fill="currentColor" stroke="none" />}
    </svg>
  )
}
