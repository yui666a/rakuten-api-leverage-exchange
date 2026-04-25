// Tiny façade over the browser Notification API + WebAudio beep so callers
// don't have to repeat the feature-detection / lifecycle dance.
//
// Why an in-memory beep instead of a static .mp3? Bundling an audio file is
// an extra build asset and the user-visible behaviour (a short distinct tone
// per event severity) doesn't justify it. WebAudio can synthesise a pleasant
// 100-200ms tone on-the-fly with three different pitches for info/warning/
// critical. If the user hates it later we can swap in a static asset behind
// the same playBeep() call without touching consumers.

let cachedAudioCtx: AudioContext | null = null

function getAudioContext(): AudioContext | null {
  if (typeof window === 'undefined') return null
  // Some browsers (Safari, older Edge) only expose the prefixed constructor.
  const Ctor =
    window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!Ctor) return null
  if (!cachedAudioCtx || cachedAudioCtx.state === 'closed') {
    cachedAudioCtx = new Ctor()
  }
  return cachedAudioCtx
}

export type BeepKind = 'info' | 'success' | 'warning' | 'critical'

const beepFreq: Record<BeepKind, number[]> = {
  info: [660],
  success: [880, 1320],
  warning: [880, 660],
  critical: [660, 440, 660, 440],
}

export function playBeep(kind: BeepKind = 'info') {
  const ctx = getAudioContext()
  if (!ctx) return
  // Auto-resume if the AudioContext was suspended (Chrome auto-suspends until
  // first user gesture; calling resume() inside a click handler upstream is
  // enough for it to work for the rest of the session).
  if (ctx.state === 'suspended') {
    void ctx.resume()
  }
  const freqs = beepFreq[kind] ?? beepFreq.info
  const stepMs = 130
  const now = ctx.currentTime
  freqs.forEach((freq, i) => {
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = 'sine'
    osc.frequency.value = freq
    const start = now + (i * stepMs) / 1000
    const end = start + 0.1
    gain.gain.setValueAtTime(0.0001, start)
    gain.gain.exponentialRampToValueAtTime(0.18, start + 0.01)
    gain.gain.exponentialRampToValueAtTime(0.0001, end)
    osc.connect(gain).connect(ctx.destination)
    osc.start(start)
    osc.stop(end + 0.02)
  })
}

export type ShowNotificationOptions = {
  title: string
  body: string
  icon?: string
  tag?: string // dedupe key — same tag replaces the previous notification
  silent?: boolean
}

export function canShowNotification(): boolean {
  return (
    typeof window !== 'undefined' &&
    'Notification' in window &&
    window.Notification.permission === 'granted'
  )
}

export function showNotification(opts: ShowNotificationOptions): Notification | null {
  if (!canShowNotification()) return null
  try {
    return new Notification(opts.title, {
      body: opts.body,
      icon: opts.icon,
      tag: opts.tag,
      // Always silent at the OS level — we play our own beep separately so
      // the user can mute audio without losing the visual notification.
      silent: true,
    })
  } catch {
    return null
  }
}
