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

// lightweight-charts は UTCTimestamp を UTC 基準で描画する。
// このプロダクトは JST 固定の市場 (楽天ウォレット) を扱うため、
// 軸ラベル・クロスヘアの時刻を JST 表記に揃える。
const JST_OFFSET_MS = 9 * 60 * 60 * 1000

function toJstParts(unixSeconds: number) {
  const d = new Date(unixSeconds * 1000 + JST_OFFSET_MS)
  return {
    year: d.getUTCFullYear(),
    month: d.getUTCMonth() + 1,
    day: d.getUTCDate(),
    hour: d.getUTCHours(),
    minute: d.getUTCMinutes(),
  }
}

const pad2 = (n: number) => String(n).padStart(2, '0')

// 軸目盛 (短い表記)。1 日以上の足は日付を、それ未満は時刻を返す。
export function formatChartTickJst(unixSeconds: number, withDate: boolean): string {
  const { month, day, hour, minute } = toJstParts(unixSeconds)
  if (withDate) return `${month}/${day}`
  return `${pad2(hour)}:${pad2(minute)}`
}

// クロスヘア・ツールチップ用 (フル表記)。
export function formatChartTimeJst(unixSeconds: number): string {
  const { year, month, day, hour, minute } = toJstParts(unixSeconds)
  return `${year}-${pad2(month)}-${pad2(day)} ${pad2(hour)}:${pad2(minute)} JST`
}
