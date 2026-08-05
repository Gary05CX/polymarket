package strategy

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

func testFV() config.FairValueConfig {
	return config.FairValueConfig{
		PMinDec: decimal.RequireFromString("0.12"),
		PMaxDec: decimal.RequireFromString("0.88"),
	}
}

func TestStrategyATriggersInBand(t *testing.T) {
	e := New(config.StrategyAConfig{
		Enabled:           true,
		PriceMinDec:       decimal.RequireFromString("0.58"),
		PriceMaxDec:       decimal.RequireFromString("0.64"),
		MinEdgeDec:        decimal.RequireFromString("0.04"),
		MaxEdgeDec:        decimal.RequireFromString("0.12"),
		EdgeSlopeDec:      decimal.Zero,
		RejectFairAtClamp: true,
		SizeMinUSDDec:     decimal.RequireFromString("1"),
		SizeMaxUSDDec:     decimal.RequireFromString("3"),
		MinSecondsLeft:    60,
		LimitPriceMode:    "mid",
	}, config.StrategyBConfig{Enabled: false}, testFV())

	in := MarketInput{
		Slug:        "btc-updown-5m-1",
		UpTokenID:   "up",
		DownTokenID: "down",
		MidUp:       decimal.RequireFromString("0.62"),
		FairUp:      decimal.RequireFromString("0.70"),
		EdgeUp:      decimal.RequireFromString("0.08"),
		MidDown:     decimal.RequireFromString("0.60"),
		FairDown:    decimal.RequireFromString("0.65"),
		EdgeDown:    decimal.RequireFromString("0.05"),
		SecondsLeft: 120,
	}
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Strategy != "A" || sigs[0].Outcome != SideUp {
		t.Fatalf("expected one A/Up signal (max edge), got %+v", sigs)
	}
}

func TestStrategyARejectsFairClamp(t *testing.T) {
	e := New(config.StrategyAConfig{
		Enabled:           true,
		PriceMinDec:       decimal.RequireFromString("0.58"),
		PriceMaxDec:       decimal.RequireFromString("0.64"),
		MinEdgeDec:        decimal.RequireFromString("0.04"),
		MaxEdgeDec:        decimal.RequireFromString("0.12"),
		RejectFairAtClamp: true,
		SizeMinUSDDec:     decimal.RequireFromString("1"),
		SizeMaxUSDDec:     decimal.RequireFromString("3"),
		MinSecondsLeft:    60,
		LimitPriceMode:    "mid",
	}, config.StrategyBConfig{Enabled: false}, testFV())

	in := MarketInput{
		UpTokenID:   "up",
		MidUp:       decimal.RequireFromString("0.62"),
		FairUp:      decimal.RequireFromString("0.88"), // at p_max
		EdgeUp:      decimal.RequireFromString("0.26"),
		SecondsLeft: 120,
	}
	sigs, skips := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("expected no signal at fair clamp, got %+v", sigs)
	}
	if len(skips) == 0 {
		t.Fatal("expected skip reason for fair clamp")
	}
}

func TestStrategyARejectsNoEdge(t *testing.T) {
	e := New(config.StrategyAConfig{
		Enabled:        true,
		PriceMinDec:    decimal.RequireFromString("0.58"),
		PriceMaxDec:    decimal.RequireFromString("0.64"),
		MinEdgeDec:     decimal.RequireFromString("0.04"),
		SizeMinUSDDec:  decimal.RequireFromString("1"),
		SizeMaxUSDDec:  decimal.RequireFromString("3"),
		MinSecondsLeft: 60,
	}, config.StrategyBConfig{Enabled: false}, testFV())

	in := MarketInput{
		UpTokenID:   "up",
		MidUp:       decimal.RequireFromString("0.62"),
		FairUp:      decimal.RequireFromString("0.64"),
		EdgeUp:      decimal.RequireFromString("0.02"),
		SecondsLeft: 120,
	}
	if sigs, _ := e.Evaluate(in); len(sigs) != 0 {
		t.Fatalf("expected no signal, got %+v", sigs)
	}
}

func TestStrategyBOnSpotMove(t *testing.T) {
	e := New(config.StrategyAConfig{Enabled: false}, config.StrategyBConfig{
		Enabled:               true,
		PriceMoveThresholdDec: decimal.RequireFromString("0.001"),
		MaxSizeUSDDec:         decimal.RequireFromString("5"),
		MinEdgeDec:            decimal.RequireFromString("0.04"),
		MaxMarketPriceDec:     decimal.RequireFromString("0.75"),
		MinSecondsLeft:        30,
		LimitPriceMode:        "mid",
		RejectFairAtClamp:     true,
	}, testFV())

	in := MarketInput{
		Slug:        "btc-updown-5m-1",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("100"),
		Spot:        decimal.RequireFromString("100.5"), // +0.5%
		MidUp:       decimal.RequireFromString("0.55"),
		FairUp:      decimal.RequireFromString("0.70"),
		EdgeUp:      decimal.RequireFromString("0.15"),
		SecondsLeft: 90,
	}
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Strategy != "B" || sigs[0].Outcome != SideUp {
		t.Fatalf("expected B/Up, got %+v", sigs)
	}
}

func TestStrategyBSkipMidTooHigh(t *testing.T) {
	e := New(config.StrategyAConfig{Enabled: false}, config.StrategyBConfig{
		Enabled:               true,
		PriceMoveThresholdDec: decimal.RequireFromString("0.001"),
		MaxSizeUSDDec:         decimal.RequireFromString("5"),
		MinEdgeDec:            decimal.RequireFromString("0.04"),
		MaxMarketPriceDec:     decimal.RequireFromString("0.75"),
		MinSecondsLeft:        30,
		LimitPriceMode:        "mid",
	}, testFV())

	in := MarketInput{
		Slug:        "m",
		UpTokenID:   "up",
		OpenPrice:   decimal.RequireFromString("100"),
		Spot:        decimal.RequireFromString("100.5"),
		MidUp:       decimal.RequireFromString("0.90"),
		FairUp:      decimal.RequireFromString("0.95"),
		EdgeUp:      decimal.RequireFromString("0.05"),
		SecondsLeft: 90,
	}
	sigs, skips := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("expected no signal, got %+v", sigs)
	}
	found := false
	for _, s := range skips {
		if s.Strategy == "B" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected B skip, got %+v", skips)
	}
}

func TestRequiredEdgeSlope(t *testing.T) {
	a := config.StrategyAConfig{
		MinEdgeDec:   decimal.RequireFromString("0.04"),
		PriceMinDec:  decimal.RequireFromString("0.58"),
		EdgeSlopeDec: decimal.RequireFromString("0.5"),
	}
	// mid 0.62 → extra 0.02 → req 0.06
	req := requiredEdgeA(a, decimal.RequireFromString("0.62"))
	want := decimal.RequireFromString("0.06")
	if !req.Equal(want) {
		t.Fatalf("req=%s want=%s", req, want)
	}
}
