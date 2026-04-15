import type { IndicatorSet } from '../lib/api'

type IndicatorPanelProps = {
  indicators: IndicatorSet | undefined
}

type IndicatorDescription = {
  title: string
  description: string
  reading: string
}

const indicatorDescriptions: Record<string, IndicatorDescription> = {
  rsi14: {
    title: 'RSI (Relative Strength Index)',
    description:
      '直近14期間の値上がり幅と値下がり幅の比率から算出されるオシレーター指標。0〜100の範囲で推移し、相場の「買われすぎ・売られすぎ」を判断します。',
    reading:
      '一般的に70以上で買われすぎ（売りシグナル）、30以下で売られすぎ（買いシグナル）と判断します。',
  },
  sma20: {
    title: 'SMA(20) — 単純移動平均線（短期）',
    description:
      '直近20期間の終値を単純に平均した値。短期のトレンド方向を確認するために使用します。',
    reading:
      '価格がSMA(20)より上なら短期上昇トレンド、下なら短期下降トレンドの可能性があります。',
  },
  sma50: {
    title: 'SMA(50) — 単純移動平均線（中期）',
    description:
      '直近50期間の終値を単純に平均した値。中期のトレンド方向を確認するために使用します。',
    reading:
      'SMA(20)がSMA(50)を上抜けるとゴールデンクロス（買いシグナル）、下抜けるとデッドクロス（売りシグナル）とされます。',
  },
  ema12: {
    title: 'EMA(12) — 指数移動平均線（短期）',
    description:
      '直近の価格に高い重みを付けた12期間の移動平均。SMAより価格変動に敏感に反応します。MACDの計算にも使われます。',
    reading:
      'EMA(12)がEMA(26)より上にあれば短期的な上昇モメンタム、下にあれば下降モメンタムを示します。',
  },
  ema26: {
    title: 'EMA(26) — 指数移動平均線（中期）',
    description:
      '直近の価格に高い重みを付けた26期間の移動平均。EMA(12)とともにMACDの算出に使われます。',
    reading:
      'EMA(12)との乖離幅がそのままMACDの値になります。乖離が広がるほどトレンドの勢いが強いことを示します。',
  },
  macdLine: {
    title: 'MACD (移動平均収束拡散法)',
    description:
      'EMA(12) − EMA(26) で算出される値。短期と中期の移動平均の差を追うことでトレンドの方向と勢いを把握します。',
    reading:
      'MACDがシグナルラインを上抜けると買いシグナル、下抜けると売りシグナルとされます。ゼロラインとの位置関係もトレンドの判断材料です。',
  },
  signalLine: {
    title: 'Signal Line（シグナルライン）',
    description:
      'MACDの9期間EMA。MACDの動きを滑らかにしたもので、MACDとの交差がシグナルになります。',
    reading:
      'MACDがシグナルを上抜ける → 買い（ゴールデンクロス）、下抜ける → 売り（デッドクロス）の判断に使います。',
  },
  histogram: {
    title: 'MACD Histogram（ヒストグラム）',
    description:
      'MACD − Signal Line で算出される棒グラフ。MACDとシグナルラインの乖離幅を可視化します。',
    reading:
      'ヒストグラムがゼロから離れるほどトレンドの勢いが強く、ゼロに近づくとトレンド転換の可能性があります。プラスに転じた瞬間は下落勢い鈍化のサインです。',
  },
}

function formatNum(v: number | null, decimals = 0): string {
  if (v === null || v === undefined) return '\u2014'
  return v.toLocaleString('ja-JP', { maximumFractionDigits: decimals })
}

function IndicatorRow({
  id,
  label,
  value,
  info,
}: {
  id: string
  label: string
  value: string
  info: IndicatorDescription
}) {
  const popoverId = `popover-${id}`

  return (
    <div className="flex items-center justify-between">
      <button
        type="button"
        popoverTarget={popoverId}
        className="text-text-secondary underline decoration-dotted underline-offset-4 decoration-text-secondary/40 cursor-help transition-colors hover:text-cyan-200 hover:decoration-cyan-200/50"
      >
        {label}
      </button>
      <span>{value}</span>

      {/* Popover API — ネイティブのポップオーバー */}
      <div
        id={popoverId}
        popover="auto"
        className="m-auto max-w-sm rounded-xl border border-white/10 bg-bg-card/95 p-5 text-sm text-text-primary shadow-2xl backdrop-blur-md [&::backdrop]{bg-black/40}"
      >
        <h3 className="text-base font-semibold text-cyan-200 mb-2">{info.title}</h3>
        <p className="leading-6 text-slate-300 mb-3">{info.description}</p>
        <div className="rounded-lg bg-white/5 px-3 py-2">
          <p className="text-xs font-medium text-accent-green mb-1">読み方</p>
          <p className="leading-5 text-slate-300 text-xs">{info.reading}</p>
        </div>
      </div>
    </div>
  )
}

export function IndicatorPanel({ indicators }: IndicatorPanelProps) {
  if (!indicators) {
    return (
      <div className="bg-bg-card rounded-lg p-4">
        <div className="text-text-secondary text-xs mb-2">テクニカル指標</div>
        <div className="text-text-secondary text-sm">読み込み中...</div>
      </div>
    )
  }

  return (
    <div className="bg-bg-card rounded-lg p-4">
      <div className="text-text-secondary text-xs mb-3">
        テクニカル指標
        <span className="ml-2 text-[10px] text-text-secondary/60">※ラベルをタップで解説</span>
      </div>
      <div className="space-y-2 text-sm">
        <IndicatorRow
          id="rsi14"
          label="RSI(14)"
          value={formatNum(indicators.rsi14, 1)}
          info={indicatorDescriptions.rsi14}
        />
        <IndicatorRow
          id="sma20"
          label="SMA(20)"
          value={formatNum(indicators.sma20)}
          info={indicatorDescriptions.sma20}
        />
        <IndicatorRow
          id="sma50"
          label="SMA(50)"
          value={formatNum(indicators.sma50)}
          info={indicatorDescriptions.sma50}
        />
        <IndicatorRow
          id="ema12"
          label="EMA(12)"
          value={formatNum(indicators.ema12)}
          info={indicatorDescriptions.ema12}
        />
        <IndicatorRow
          id="ema26"
          label="EMA(26)"
          value={formatNum(indicators.ema26)}
          info={indicatorDescriptions.ema26}
        />
        <div className="border-t border-bg-card-hover my-2" />
        <IndicatorRow
          id="macdLine"
          label="MACD"
          value={formatNum(indicators.macdLine, 2)}
          info={indicatorDescriptions.macdLine}
        />
        <IndicatorRow
          id="signalLine"
          label="Signal"
          value={formatNum(indicators.signalLine, 2)}
          info={indicatorDescriptions.signalLine}
        />
        <IndicatorRow
          id="histogram"
          label="Histogram"
          value={formatNum(indicators.histogram, 2)}
          info={indicatorDescriptions.histogram}
        />
      </div>
    </div>
  )
}
