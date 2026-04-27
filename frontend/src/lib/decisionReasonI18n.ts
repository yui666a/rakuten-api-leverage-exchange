const STATIC_REASONS: Record<string, string> = {
  // --- TREND_FOLLOW ---
  'trend follow: disabled by profile': '順張り: プロファイルで無効化',
  'trend follow: ADX below threshold': '順張り: ADXがしきい値未満（トレンド不足）',
  'trend follow: EMA cross but SMA not aligned': '順張り: EMAクロス発生もSMAの方向が不一致',
  'trend follow: SMAShort < SMALong, RSI not oversold':
    '順張り: 短期SMA<長期SMAだがRSIは売られすぎでない',
  'trend follow: SMAShort > SMALong, RSI not overbought':
    '順張り: 短期SMA>長期SMAだがRSIは買われすぎでない',
  'trend follow: MACD histogram negative, skipping buy':
    '順張り: MACDヒストグラムが陰、買い見送り',
  'trend follow: MACD histogram positive, skipping sell':
    '順張り: MACDヒストグラムが陽、売り見送り',
  'trend follow: no clear signal': '順張り: 明確なシグナルなし',
  'trend follow': '順張り',
  trend: 'トレンド',
  'ema cross': 'EMAクロス',

  // --- CONTRARIAN ---
  'contrarian: disabled by profile': '逆張り: プロファイルで無効化',
  'contrarian: RSI in neutral zone': '逆張り: RSIが中立ゾーン',
  'contrarian: RSI oversold but MACD momentum still strongly negative':
    '逆張り: RSI売られすぎだがMACDモメンタムが強い陰',
  'contrarian: RSI overbought but MACD momentum still strongly positive':
    '逆張り: RSI買われすぎだがMACDモメンタムが強い陽',
  'contrarian: RSI oversold, expecting bounce, MACD not strongly against':
    '逆張り: RSI売られすぎで反発期待、MACDの逆向きは弱い',
  'contrarian: RSI overbought, expecting pullback, MACD not strongly against':
    '逆張り: RSI買われすぎで押し目期待、MACDの逆向きは弱い',
  contrarian: '逆張り',
  'rsi extreme': 'RSI極端',

  // --- BREAKOUT ---
  'breakout: disabled by profile': 'ブレイク: プロファイルで無効化',
  'breakout: insufficient BB/volume data': 'ブレイク: BB/出来高データ不足',
  'breakout: MACD histogram negative, skipping buy':
    'ブレイク: MACDヒストグラムが陰、買い見送り',
  'breakout: MACD histogram positive, skipping sell':
    'ブレイク: MACDヒストグラムが陽、売り見送り',
  'breakout: price above BB upper with volume confirmation':
    'ブレイク: BB上限を出来高伴って上抜け',
  'breakout: price below BB lower with volume confirmation':
    'ブレイク: BB下限を出来高伴って下抜け',
  'breakout: no clear breakout signal': 'ブレイク: 明確なブレイクなし',

  // --- 共通シグナル ---
  'stance is HOLD': 'スタンスがHOLD',
  'signal is HOLD, no action': 'シグナルHOLD、アクションなし',
  'insufficient indicator data': '指標データ不足',
  'volume filter: volume ratio too low, signal unreliable':
    '出来高フィルタ: 出来高比が低くシグナル信頼性不足',
  'MTF filter: higher timeframe inside cloud blocks buy':
    'MTFフィルタ: 上位足が雲の中で買いブロック',
  'MTF filter: higher timeframe inside cloud blocks sell':
    'MTFフィルタ: 上位足が雲の中で売りブロック',
  'trading is manually stopped': '取引が手動停止中',

  // --- リスク（静的） ---
  'daily loss limit hit': '日次損失上限到達',
  'sizer returned zero lot': 'サイザーが0ロットを返却',

  // --- BookGate ---
  empty_book: '板が空',
  'empty book side': '板の片側が空',
  empty_book_side: '板の片側が空',
  stale_book: '板情報が古い',
  stale_book_pass: '板情報が古い（通過）',
  'no snapshot within stale window': '許容ウィンドウ内に板スナップショットなし',
  no_book: '板情報なし',
  no_book_pass: '板情報なし（通過）',
  thin_book_pre_trade: '発注前に板が薄い',
  'thin book on bid': '買い板が薄い',
  'insufficient depth': '板の厚みが不足',
  lot_exceeds_book_side_ratio: 'ロットが板厚比の上限を超過',
  slippage_exceeds_threshold: '想定スリッページが上限超過',

  // --- 注文/クローズ ---
  'REST API order': 'REST API発注',
  stop_loss: 'ストップロス',
  trailing_stop: 'トレイリングストップ',
  'circuit_breaker:price_jump': 'サーキットブレーカー: 価格急変',
}

type DynamicRule = { regex: RegExp; replace: (m: RegExpMatchArray) => string }

const DYNAMIC_RULES: DynamicRule[] = [
  {
    regex: /^position limit exceeded:\s*([\d.]+)\+([\d.]+)\s*>\s*([\d.]+)$/,
    replace: (m) => `ポジション上限超過: ${m[1]}+${m[2]} > ${m[3]}`,
  },
  {
    regex: /^insufficient balance:\s*([\d.]+)\s*>\s*([\d.]+)$/,
    replace: (m) => `残高不足: ${m[1]} > ${m[2]}`,
  },
  {
    regex: /^daily loss limit exceeded:\s*([\d.]+)\/([\d.]+)$/,
    replace: (m) => `日次損失上限超過: ${m[1]} / ${m[2]}`,
  },
  {
    regex: /^cooldown:\s*(\d+)\s*consecutive losses, trading paused until\s*(.+)$/,
    replace: (m) => `クールダウン: ${m[1]}連敗、${m[2]}まで取引停止`,
  },
  {
    regex: /^cooldown started for\s*(\d+)\s*min after\s*(\d+)\s*losses$/,
    replace: (m) => `${m[2]}連敗のため${m[1]}分のクールダウン開始`,
  },
  {
    regex: /^(\d+)\s*consecutive losses$/,
    replace: (m) => `${m[1]}連敗`,
  },
  {
    regex: /^MaxDD warning:\s*([\d.]+)%\s*\(peak\s*([\d.]+)\s*→\s*([\d.]+)\)$/,
    replace: (m) => `最大DD警告: ${m[1]}% (ピーク ${m[2]} → ${m[3]})`,
  },
  {
    regex: /^MaxDD critical:\s*([\d.]+)%\s*\(peak\s*([\d.]+)\s*→\s*([\d.]+)\)$/,
    replace: (m) => `最大DD危険: ${m[1]}% (ピーク ${m[2]} → ${m[3]})`,
  },
  {
    regex: /^daily loss reached\s*([\d.]+)\s*\/\s*max\s*([\d.]+)\s*\(([\d.]+)%\)$/,
    replace: (m) => `日次損失 ${m[1]} / 上限 ${m[2]} (${m[3]}%) に到達`,
  },
  {
    regex: /^sizer skipped:\s*invalid input:\s*equity=(\S+)\s*price=(\S+)\s*sl=(\S+)$/,
    replace: (m) => `サイザー停止: 入力不正 (equity=${m[1]} price=${m[2]} sl=${m[3]})`,
  },
  {
    regex: /^sizer skipped:\s*computed lot\s*([\d.]+)\s*below min_lot\s*([\d.]+)$/,
    replace: (m) => `サイザー停止: 算出ロット ${m[1]} が最小ロット ${m[2]} 未満`,
  },
  {
    regex: /^sizer skipped:\s*(.+)$/,
    replace: (m) => `サイザー停止: ${m[1]}`,
  },
  {
    regex: /^risk rejected close:\s*(.+)$/,
    replace: (m) => `クローズリスク却下: ${translateReason(m[1])}`,
  },
  {
    regex: /^risk rejected:\s*(.+)$/,
    replace: (m) => `リスク却下: ${translateReason(m[1])}`,
  },
]

export function translateReason(reason: string | undefined | null): string {
  if (!reason) return '—'
  const trimmed = reason.trim()
  if (trimmed === '' || trimmed === '—') return '—'

  const fixed = STATIC_REASONS[trimmed]
  if (fixed) return fixed

  for (const rule of DYNAMIC_RULES) {
    const m = trimmed.match(rule.regex)
    if (m) return rule.replace(m)
  }

  return trimmed
}
