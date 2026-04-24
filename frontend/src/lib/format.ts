// Shared number formatters. Amount fields (crypto base units) can pick up
// IEEE-754 tails like 0.30000000000000004 when the backend computes lots
// via float math (e.g. 0.1 + 0.2 during dynamic sizing). Rendering the raw
// number surfaces that noise.
//
// Rakuten Wallet margin trading lot-step is 0.01 (BTC) or 0.1 (LTC/ETH/BCH),
// so 2 decimal places is enough to show the real precision without leaking
// float noise. Trailing zeros and a dangling decimal point are stripped so
// "0.30" → "0.3" and "5.00" → "5".

const AMOUNT_MAX_FRACTION_DIGITS = 2

export function formatAmount(value: number): string {
  if (!Number.isFinite(value)) return '—'
  const fixed = value.toFixed(AMOUNT_MAX_FRACTION_DIGITS)
  return fixed.replace(/\.?0+$/, '')
}
