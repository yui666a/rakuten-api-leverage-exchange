package indicator

import (
	"math"
	"testing"
)

func TestVolumeSMA_InsufficientData(t *testing.T) {
	result := VolumeSMA([]float64{100, 200}, 20)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for insufficient data, got %f", result)
	}
}

func TestVolumeSMA_ExactPeriod(t *testing.T) {
	volumes := make([]float64, 20)
	for i := range volumes {
		volumes[i] = 100.0
	}
	result := VolumeSMA(volumes, 20)
	if result != 100.0 {
		t.Fatalf("expected 100.0, got %f", result)
	}
}

func TestVolumeSMA_UsesLastNPeriod(t *testing.T) {
	// 30 candles, last 20 are all 200, first 10 are all 100
	volumes := make([]float64, 30)
	for i := range volumes {
		if i < 10 {
			volumes[i] = 100.0
		} else {
			volumes[i] = 200.0
		}
	}
	result := VolumeSMA(volumes, 20)
	if result != 200.0 {
		t.Fatalf("expected 200.0, got %f", result)
	}
}

func TestVolumeRatio_Normal(t *testing.T) {
	// Current volume = 300, SMA = 100 → ratio = 3.0
	result := VolumeRatio(300, 100)
	if result != 3.0 {
		t.Fatalf("expected 3.0, got %f", result)
	}
}

func TestVolumeRatio_ZeroSMA(t *testing.T) {
	result := VolumeRatio(300, 0)
	if !math.IsNaN(result) {
		t.Fatalf("expected NaN for zero SMA, got %f", result)
	}
}

func TestVolumeRatio_ZeroVolume(t *testing.T) {
	result := VolumeRatio(0, 100)
	if result != 0.0 {
		t.Fatalf("expected 0.0, got %f", result)
	}
}
