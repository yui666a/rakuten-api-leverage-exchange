package decision

import (
	"context"
	"testing"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/usecase/eventengine"
)

// stubExecutor satisfies eventengine.OrderExecutor by returning a pre-loaded
// position list. The executor's other methods are unused by ExecutorPositionView
// so they panic if called by mistake.
type stubExecutor struct {
	positions []eventengine.Position
}

func (s stubExecutor) Open(symbolID int64, side entity.OrderSide, signalPrice, amount float64, reason string, timestamp int64) (entity.OrderEvent, error) {
	panic("unused")
}

func (s stubExecutor) Close(positionID int64, signalPrice float64, reason string, timestamp int64) (entity.OrderEvent, *entity.BacktestTradeRecord, error) {
	panic("unused")
}

func (s stubExecutor) Positions() []eventengine.Position { return s.positions }

func (s stubExecutor) SelectSLTPExit(side entity.OrderSide, stopLossPrice, takeProfitPrice, barLow, barHigh float64) (float64, string, bool) {
	panic("unused")
}

func TestExecutorPositionView_NetSide(t *testing.T) {
	cases := []struct {
		name      string
		positions []eventengine.Position
		symbol    int64
		want      entity.OrderSide
	}{
		{"flat", nil, 7, ""},
		{"single long", []eventengine.Position{{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 1.0}}, 7, entity.OrderSideBuy},
		{"single short", []eventengine.Position{{SymbolID: 7, Side: entity.OrderSideSell, Amount: 1.0}}, 7, entity.OrderSideSell},
		{
			"two longs sum", []eventengine.Position{
				{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.5},
				{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.7},
			}, 7, entity.OrderSideBuy,
		},
		{
			"net long with both sides", []eventengine.Position{
				{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 1.0},
				{SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.3},
			}, 7, entity.OrderSideBuy,
		},
		{
			"net short with both sides", []eventengine.Position{
				{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.4},
				{SymbolID: 7, Side: entity.OrderSideSell, Amount: 1.0},
			}, 7, entity.OrderSideSell,
		},
		{
			"perfectly hedged is flat", []eventengine.Position{
				{SymbolID: 7, Side: entity.OrderSideBuy, Amount: 0.5},
				{SymbolID: 7, Side: entity.OrderSideSell, Amount: 0.5},
			}, 7, "",
		},
		{
			"other symbol ignored", []eventengine.Position{
				{SymbolID: 99, Side: entity.OrderSideBuy, Amount: 5.0},
				{SymbolID: 7, Side: entity.OrderSideSell, Amount: 1.0},
			}, 7, entity.OrderSideSell,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := ExecutorPositionView{Executor: stubExecutor{positions: c.positions}}
			if got := v.CurrentSide(context.Background(), c.symbol); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestExecutorPositionView_NilExecutorIsFlat(t *testing.T) {
	v := ExecutorPositionView{Executor: nil}
	if got := v.CurrentSide(context.Background(), 7); got != "" {
		t.Errorf("nil executor should be flat, got %q", got)
	}
}
