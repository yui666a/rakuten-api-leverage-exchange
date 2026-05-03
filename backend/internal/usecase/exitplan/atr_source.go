package exitplan

import (
	"context"
	"math"
	"sync"

	"github.com/yui666a/rakuten-api-leverage-exchange/backend/internal/domain/entity"
)

// ATRSource は IndicatorEvent から ATR を吸い上げて、TrailingHandler が
// 動的計算で参照するための in-memory 共有値を保持する。
//
// 既存 backtest.TickRiskHandler.UpdateATR と同じ受け入れ規則:
//   - NaN は無視
//   - 負値は無視
//   - 0 は受け入れる（ボラ消失からの復帰時に stale positive ATR が残らない）
type ATRSource struct {
	mu  sync.RWMutex
	atr float64
}

// NewATRSource はゼロ初期状態の ATRSource を返す。
func NewATRSource() *ATRSource {
	return &ATRSource{}
}

// Handle implements eventengine.EventHandler. IndicatorEvent 以外は素通り。
func (s *ATRSource) Handle(_ context.Context, ev entity.Event) ([]entity.Event, error) {
	ie, ok := ev.(entity.IndicatorEvent)
	if !ok {
		return nil, nil
	}
	if ie.Primary.ATR == nil {
		return nil, nil
	}
	v := *ie.Primary.ATR
	if math.IsNaN(v) || v < 0 {
		return nil, nil
	}
	s.mu.Lock()
	s.atr = v
	s.mu.Unlock()
	return nil, nil
}

// Current は現在の ATR を返す。スレッドセーフ。
func (s *ATRSource) Current() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.atr
}
