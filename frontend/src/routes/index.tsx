import { createFileRoute, useSearch } from "@tanstack/react-router";
import { AppFrame } from "../components/AppFrame";
import { KpiCard } from "../components/KpiCard";
import { CandlestickChart } from "../components/CandlestickChart";
import { IndicatorPanel } from "../components/IndicatorPanel";
import { PositionPanel } from "../components/PositionPanel";
import { BotControlCard } from "../components/BotControlCard";
import { LiveTickerCard } from "../components/LiveTickerCard";
import { ManualTradeCard } from "../components/ManualTradeCard";
import { OrderbookPanel } from "../components/OrderbookPanel";
import { ExecutionQualityCard } from "../components/ExecutionQualityCard";
import { HaltReasonBadge } from "../components/HaltReasonBadge";
import { RecentDecisionsCard } from "../components/RecentDecisionsCard";
import { useStatus } from "../hooks/useStatus";
import { usePnl } from "../hooks/usePnl";
import { useStrategy } from "../hooks/useStrategy";
import { useIndicators } from "../hooks/useIndicators";
import { usePositions } from "../hooks/usePositions";
import { useStartBot, useStopBot } from "../hooks/useBotControl";
import { useMarketTickerStream } from "../hooks/useMarketTickerStream";
import { useSymbolContext } from "../contexts/SymbolContext";

export const Route = createFileRoute("/")({ component: Dashboard });

function Dashboard() {
  const { symbolId, currentSymbol } = useSymbolContext();
  const rootSearch = useSearch({ from: "__root__" }) as { symbol?: string };
  const { data: status } = useStatus();
  const { data: pnl } = usePnl();
  const { data: strategy } = useStrategy();
  const { data: indicators } = useIndicators(symbolId);
  const { data: positions } = usePositions(symbolId);
  const startBot = useStartBot();
  const stopBot = useStopBot();
  const { ticker, orderbook, connectionState } =
    useMarketTickerStream(symbolId);

  const dailyPnlTotal = pnl?.dailyPnl?.total ?? null;
  const dailyPnlStale = pnl?.dailyPnl?.stale ?? false;
  const dailyPnlLabel =
    dailyPnlTotal === null
      ? "—"
      : `${dailyPnlTotal < 0 ? "-" : ""}¥${Math.abs(dailyPnlTotal).toLocaleString()}${dailyPnlStale ? "*" : ""}`;

  const balance = pnl?.balance ?? null;
  const positionValue = pnl?.totalPosition ?? null;
  const floatingPnl =
    positions?.reduce((sum, p) => sum + p.floatingProfit, 0) ?? null;
  const totalEquity =
    balance !== null && floatingPnl !== null ? balance + floatingPnl : null;
  const freeBalance =
    balance !== null && positionValue !== null
      ? balance - positionValue
      : null;

  const formatYen = (v: number | null) =>
    v === null
      ? "—"
      : `${v < 0 ? "-" : ""}¥${Math.abs(Math.round(v)).toLocaleString()}`;
  const formatSignedYen = (v: number | null) =>
    v === null
      ? "—"
      : `${v < 0 ? "-" : "+"}¥${Math.abs(Math.round(v)).toLocaleString()}`;

  return (
    <AppFrame
      title="トレーディングダッシュボード"
      subtitle="KPI・戦略・ポジション・操作系を集約した監視画面です。"
    >
      <HaltReasonBadge
        haltReason={status?.haltReason}
        manuallyStopped={status?.manuallyStopped}
        tradingHalted={status?.tradingHalted}
      />

      <div className="mt-3">
        <RecentDecisionsCard
          symbolId={symbolId}
          strategy={strategy}
          rootSearch={rootSearch}
        />
      </div>

      <div className="mt-4">
        <p className="mb-2 text-[0.65rem] uppercase tracking-[0.32em] text-text-secondary">
          資金状況
        </p>
        <div className="grid grid-cols-2 gap-3 sm:gap-4 sm:grid-cols-3 xl:grid-cols-5">
          <KpiCard
            label="総資産"
            value={formatYen(totalEquity)}
            color="text-accent-green"
          />
          <KpiCard
            label="建玉評価額"
            value={formatYen(positionValue)}
            color="text-text-primary"
          />
          <KpiCard
            label="拘束外残高"
            value={formatYen(freeBalance)}
            color="text-cyan-200"
          />
          <KpiCard
            label="含み損益"
            value={formatSignedYen(floatingPnl)}
            color={
              floatingPnl !== null && floatingPnl < 0
                ? "text-accent-red"
                : "text-accent-green"
            }
          />
          <KpiCard
            label="日次損益"
            value={dailyPnlLabel}
            color={
              dailyPnlTotal !== null && dailyPnlTotal < 0
                ? "text-accent-red"
                : "text-accent-green"
            }
          />
        </div>
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)]">
        <section className="space-y-4">
          <LiveTickerCard
            ticker={ticker}
            orderbook={orderbook}
            connectionState={connectionState}
            currencyPair={currentSymbol?.currencyPair?.replace("_", "/")}
          />
          <CandlestickChart symbolId={symbolId} />
        </section>

        <aside className="space-y-4">
          <BotControlCard
            status={status}
            onStart={() => startBot.mutate()}
            onStop={() => stopBot.mutate()}
            isPending={startBot.isPending || stopBot.isPending}
          />
          <OrderbookPanel
            orderbook={orderbook}
            currencyPair={currentSymbol?.currencyPair}
            microprice={indicators?.microprice ?? null}
            ofiShort={indicators?.ofiShort ?? null}
            ofiLong={indicators?.ofiLong ?? null}
          />
          <ExecutionQualityCard />
          <ManualTradeCard
            symbolId={symbolId}
            currencyPair={currentSymbol?.currencyPair}
            lotStep={currentSymbol?.baseStepAmount}
            minLot={currentSymbol?.minOrderAmount}
          />
          <IndicatorPanel indicators={indicators} />
          <PositionPanel positions={positions} />
        </aside>
      </div>
    </AppFrame>
  );
}
