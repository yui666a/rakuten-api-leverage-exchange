type StanceEntry = {
  key: "TREND_FOLLOW" | "CONTRARIAN" | "BREAKOUT" | "HOLD";
  title: string;
  summary: string;
  buy: string;
  sell: string;
};

const STANCE_ENTRIES: StanceEntry[] = [
  {
    key: "TREND_FOLLOW",
    title: "TREND_FOLLOW（順張り）",
    summary:
      "SMA / EMA のトレンド方向に沿ってエントリーする方針。市場が一方向に動いているときに乗る。",
    buy: "EMA12 が EMA26 を上抜け + SMA20 > SMA50 で BUY",
    sell: "EMA12 が EMA26 を下抜け + SMA20 < SMA50 で SELL",
  },
  {
    key: "CONTRARIAN",
    title: "CONTRARIAN（逆張り）",
    summary:
      '行き過ぎた価格の反発・反落を狙う方針。"群衆と逆方向" に張る。CONTRARIAN だから売る、ではない点に注意。',
    buy: "RSI が oversold 閾値（例: 32）を下回ったら反発期待で BUY",
    sell: "RSI が overbought 閾値（例: 68）を上回ったら反落期待で SELL",
  },
  {
    key: "BREAKOUT",
    title: "BREAKOUT（抜け追い）",
    summary:
      "BB スクイーズ解消 + 出来高急増のときに、抜けた方向へ追随する方針。",
    buy: "価格が BB Upper を上抜け + VolumeRatio が閾値以上で BUY",
    sell: "価格が BB Lower を下抜け + VolumeRatio が閾値以上で SELL",
  },
  {
    key: "HOLD",
    title: "HOLD（待機）",
    summary:
      "トレンド不明瞭・指標発表直前など、エントリー条件を満たさないときの待機状態。新規シグナルは出ない。",
    buy: "—",
    sell: "—",
  },
];

const POPOVER_ID = "stance-legend-popover";

export function StanceLegendPopover() {
  return (
    <>
      <button
        type="button"
        // @ts-expect-error popovertarget is part of the HTML Popover API; not yet in React's typed attribute set.
        popovertarget={POPOVER_ID}
        aria-label="戦略方針の説明を表示"
        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-white/20 text-[10px] font-semibold text-text-secondary transition hover:border-cyan-300 hover:text-cyan-200"
      >
        ?
      </button>
      <div
        id={POPOVER_ID}
        popover="auto"
        className="m-0 w-[min(420px,calc(100vw-2rem))] rounded-2xl border border-white/10 bg-bg-card/95 p-5 text-left text-sm text-text-primary shadow-[0_24px_64px_rgba(0,0,0,0.45)] backdrop-blur"
      >
        <h3 className="text-base font-semibold text-white">
          戦略方針 (Stance) について
        </h3>
        <p className="mt-1 text-xs text-text-secondary">
          指標から自動判定される現在の方針。各 stance ごとに BUY / SELL
          の出方が変わります。
        </p>
        <ul className="mt-4 space-y-3">
          {STANCE_ENTRIES.map((s) => (
            <li
              key={s.key}
              className="rounded-xl border border-white/8 bg-white/[0.02] p-3"
            >
              <p className="text-sm font-semibold text-cyan-200">{s.title}</p>
              <p className="mt-1 text-xs leading-5 text-text-secondary">
                {s.summary}
              </p>
              {s.buy !== "—" || s.sell !== "—" ? (
                <dl className="mt-2 space-y-1 text-xs leading-5">
                  <div className="flex gap-2">
                    <dt className="shrink-0 text-accent-green">BUY:</dt>
                    <dd className="text-text-primary">{s.buy}</dd>
                  </div>
                  <div className="flex gap-2">
                    <dt className="shrink-0 text-accent-red">SELL:</dt>
                    <dd className="text-text-primary">{s.sell}</dd>
                  </div>
                </dl>
              ) : null}
            </li>
          ))}
        </ul>
      </div>
    </>
  );
}
