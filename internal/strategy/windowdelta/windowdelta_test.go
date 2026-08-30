package windowdelta

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/domain"
)

func TestAnalyzeDeltaWeights(t *testing.T) {
	cfg := config.WindowDelta{DeltaSkip: 0.0005, DeltaWeak: 0.001, DeltaStrong: 0.002, ATRPeriods: 5, ATRMultiplier: 1.5}
	open := 100000.0
	ta := Analyze(100200, open, nil, nil, cfg) // 0.20%
	if ta.Direction != "Up" || ta.Score < 5 {
		t.Fatalf("got %+v", ta)
	}
	ta = Analyze(99900, open, nil, nil, cfg) // -0.10%
	if ta.Direction != "Down" {
		t.Fatalf("down %+v", ta)
	}
	ta = Analyze(100020, open, nil, nil, cfg) // 0.02% skip
	if ta.SkipCode != "delta_small" {
		t.Fatalf("skip %+v", ta)
	}
}

func TestMomentumAdds(t *testing.T) {
	cfg := config.WindowDelta{DeltaSkip: 0.0005, DeltaWeak: 0.001, DeltaStrong: 0.002}
	k := []domain.Candle{{Close: 100}, {Close: 101}}
	ta := Analyze(100200, 100000, k, nil, cfg)
	if ta.Score != 7 { // 5 + 2
		t.Fatalf("score %v", ta.Score)
	}
}
