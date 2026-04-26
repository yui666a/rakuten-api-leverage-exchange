package main

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestPipelineForConcurrency は並行性テスト専用の TradingPipeline を返す。
// runTradingLoop / runStopLossMonitor は依存 nil 時に即 wait するガードを持つので、
// ロック挙動の検証だけを目的にフィールドだけ初期化する。
func newTestPipelineForConcurrency(t *testing.T) *TradingPipeline {
	t.Helper()
	return &TradingPipeline{
		symbolID:    7,
		interval:    1 * time.Hour, // 評価ループを回す意図はない
		tradeAmount: 1000,
	}
}

// TestSwitchSymbol_ConcurrentStartStop は SwitchSymbol と Start/Stop が
// 並行実行されても panic せず、最終状態が一貫することを検証する。
// go test -race で実行すること。
func TestSwitchSymbol_ConcurrentStartStop(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	var wg sync.WaitGroup
	var switchCount atomic.Int64

	// 100回並行して Switch/Start/Stop を呼ぶ
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			p.SwitchSymbol(int64(7+i%3), 1000, func(oldID, newID int64) {
				// bootstrap の代わりに短い sleep で実処理を模擬
				time.Sleep(100 * time.Microsecond)
			})
			switchCount.Add(1)
		}(i)
		go func() {
			defer wg.Done()
			p.Start()
		}()
		go func() {
			defer wg.Done()
			p.Stop()
		}()
	}

	wg.Wait()

	// 最終的に Stop しておく
	p.Stop()
	if p.Running() {
		t.Errorf("pipeline should be stopped after final Stop, got Running=true")
	}
	if switchCount.Load() != 100 {
		t.Errorf("expected 100 switches, got %d", switchCount.Load())
	}
}

// TestSwitchSymbol_StopDuringSwitch は SwitchSymbol の onSwitch 実行中に
// Stop が来ても、最終的に停止状態になることを検証する。
// Codex #1 対応: switchMu が Start/Stop を直列化していなければ、
// Stop が switchMu 待ちにならず SwitchSymbol の再開フェーズで上書きされてしまう。
func TestSwitchSymbol_StopDuringSwitch(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	p.Start()
	if !p.Running() {
		t.Fatal("pipeline should be running after Start")
	}

	stopDone := make(chan struct{})
	onSwitch := func(oldID, newID int64) {
		// onSwitch 実行中に別 goroutine から Stop を叩く。
		// switchMu 保持中なので Stop は SwitchSymbol 完了まで待たされる。
		go func() {
			p.Stop()
			close(stopDone)
		}()
		// Stop が switchMu を待っている状態を作るため少し待つ
		time.Sleep(20 * time.Millisecond)
	}

	p.SwitchSymbol(8, 1000, onSwitch)

	// SwitchSymbol 完了後、Stop が switchMu を取得して実行される
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not complete within timeout")
	}

	if p.Running() {
		t.Error("pipeline should be stopped, got Running=true")
	}
	if p.SymbolID() != 8 {
		t.Errorf("symbolID should be 8 after switch, got %d", p.SymbolID())
	}
}

// TestSwitchSymbol_StartDuringSwitch は SwitchSymbol の onSwitch 実行中に
// Start が来ても、bootstrap 完了前にパイプラインが動き出さないことを検証する。
// Codex #2 対応。
func TestSwitchSymbol_StartDuringSwitch(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	// 最初は停止状態から開始（SwitchSymbol は wasRunning=false のまま終わる）
	if p.Running() {
		t.Fatal("pipeline should not be running initially")
	}

	var bootstrapDone atomic.Bool
	startDone := make(chan struct{})

	onSwitch := func(oldID, newID int64) {
		go func() {
			// Start は switchMu 待ちでブロックされ、bootstrap 完了後に走る
			p.Start()
			close(startDone)
		}()
		// Start が switchMu を待っている間に bootstrap を模擬
		time.Sleep(20 * time.Millisecond)
		bootstrapDone.Store(true)
	}

	p.SwitchSymbol(9, 1000, onSwitch)

	<-startDone

	if !bootstrapDone.Load() {
		t.Error("bootstrap should have completed before Start was able to proceed")
	}
	if !p.Running() {
		t.Error("pipeline should be running after Start")
	}
	if p.SymbolID() != 9 {
		t.Errorf("symbolID should be 9, got %d", p.SymbolID())
	}

	p.Stop()
}

// TestSwitchSymbol_PreservesRunningState は、SwitchSymbol 単独で呼んだ場合に
// 切替前の running 状態が正しく維持されることを検証する。
func TestSwitchSymbol_PreservesRunningState(t *testing.T) {
	p := newTestPipelineForConcurrency(t)

	// 停止状態での切替 → 停止のまま
	p.SwitchSymbol(10, 2000, nil)
	if p.Running() {
		t.Error("pipeline should remain stopped after switch from stopped state")
	}
	if p.SymbolID() != 10 || p.TradeAmount() != 2000 {
		t.Errorf("fields not updated: symbolID=%d, tradeAmount=%f", p.SymbolID(), p.TradeAmount())
	}

	// 起動状態での切替 → 起動のまま
	p.Start()
	p.SwitchSymbol(11, 3000, nil)
	if !p.Running() {
		t.Error("pipeline should remain running after switch from running state")
	}
	if p.SymbolID() != 11 || p.TradeAmount() != 3000 {
		t.Errorf("fields not updated: symbolID=%d, tradeAmount=%f", p.SymbolID(), p.TradeAmount())
	}

	p.Stop()
}

// TestEventDrivenPipeline_SwitchSymbol_NoOpForSameSymbolAndAmount は、
// 同じ symbol/amount への SwitchSymbol が event loop を再起動せず、onSwitch
// コールバックも呼ばない (= no-op) ことを検証する。frontend が trading-config
// を意図せず再 PUT したときに recorder の pending bar / WS subscription が
// 破壊されるのを防ぐガード。
//
// EventDrivenPipeline 本体は依存 nil で start すると即 ctx.Done を待つだけの
// goroutine を回すので、ロック/フィールドの整合だけを検証できる。
func TestEventDrivenPipeline_SwitchSymbol_NoOpForSameSymbolAndAmount(t *testing.T) {
	p := &EventDrivenPipeline{
		symbolID:    10,
		tradeAmount: 2000,
	}

	// Start で event loop goroutine を起動 (依存 nil なので即 ctx.Done 待ちで block)。
	p.Start()
	defer p.Stop()
	if !p.Running() {
		t.Fatal("pipeline must be running after Start")
	}

	switchCalled := 0
	p.SwitchSymbol(10, 2000, func(_, _ int64) { switchCalled++ })
	if switchCalled != 0 {
		t.Errorf("onSwitch must not fire for same symbol+amount, called %d times", switchCalled)
	}
	if !p.Running() {
		t.Error("pipeline must stay running across no-op switch")
	}

	// tradeAmount==0 (= "keep current") も同 symbol なら no-op。
	p.SwitchSymbol(10, 0, func(_, _ int64) { switchCalled++ })
	if switchCalled != 0 {
		t.Errorf("onSwitch must not fire for same symbol with zero amount, called %d times", switchCalled)
	}

	// 異なる amount に変える場合は通常通り switch される。
	p.SwitchSymbol(10, 3000, func(_, _ int64) { switchCalled++ })
	if switchCalled != 1 {
		t.Errorf("onSwitch must fire when amount changes, called %d times", switchCalled)
	}
	if p.TradeAmount() != 3000 {
		t.Errorf("tradeAmount should update to 3000, got %f", p.TradeAmount())
	}
}

func TestRoundDownToStep(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		step   float64
		want   float64
	}{
		{"LTC step=0.1, amount=0.1166", 0.1166, 0.1, 0.1},
		{"LTC step=0.1, amount=0.9999", 0.9999, 0.1, 0.9},
		{"BTC step=0.01, amount=0.0156", 0.0156, 0.01, 0.01},
		{"XRP step=100, amount=250", 250.0, 100.0, 200.0},
		{"ADA step=10, amount=24.8", 24.8, 10.0, 20.0},
		{"DOT step=1, amount=4.76", 4.76, 1.0, 4.0},
		{"exact match", 0.3, 0.1, 0.3},
		{"step=0 fallback to 4 decimals", 0.11667, 0, 0.1166},
		{"step negative fallback", 0.11667, -1, 0.1166},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundDownToStep(tt.amount, tt.step)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("roundDownToStep(%v, %v) = %v, want %v", tt.amount, tt.step, got, tt.want)
			}
		})
	}
}
