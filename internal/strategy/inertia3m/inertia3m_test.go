package inertia3m

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/domain"
)

func TestAnalyzeStrongUp(t *testing.T) {
	cfg := config.Inertia3m{FlatThresholdPct: 0.05, StrongMovePct: 0.10, MinScoreModerate: 4, MinScoreStrong: 5}
	kl := []domain.Candle{
		{Open: 100, Close: 100.1, Volume: 1},
		{Open: 100.1, Close: 100.2, Volume: 1},
		{Open: 100.2, Close: 100.3, Volume: 3},
	}
	ta := Analyze(kl, 100, 100.15, cfg) // +0.15%
	if ta.Side != "Up" || ta.Score < 5 {
		t.Fatalf("%+v", ta)
	}
}

func TestAnalyzeFlat(t *testing.T) {
	cfg := config.Inertia3m{FlatThresholdPct: 0.05, StrongMovePct: 0.10, MinScoreModerate: 4}
	ta := Analyze(nil, 100, 100.02, cfg)
	if ta.Side != "" {
		t.Fatalf("expected flat %+v", ta)
	}
}
