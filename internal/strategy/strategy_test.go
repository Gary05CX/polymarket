package strategy

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

func TestStrategyATriggersInBand(t *testing.T) {
	e := New(config.StrategyAConfig{
		Enabled:         true,
		PriceMinDec:     decimal.RequireFromString("0.60"),
		PriceMaxDec:     decimal.RequireFromString("0.70"),
		MinEdgeDec:      decimal.RequireFromString("0.03"),
		SizeMinUSDDec:   decimal.RequireFromString("1"),
		SizeMaxUSDDec:   decimal.RequireFromString("3"),
		MinSecondsLeft:  60,
		LimitPriceMode:  "mid",
	}, config.StrategyBConfig{Enabled: false})

	in := MarketInput{
		Slug:        "btc-updown-5m-1",
		UpTokenID:   "up",
		DownTokenID: "down",
		MidUp:       decimal.RequireFromString("0.65"),
		FairUp:      decimal.RequireFromString("0.72"),
		EdgeUp:      decimal.RequireFromString("0.07"),
		SecondsLeft: 120,
	}
	sigs := e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Strategy != "A" || sigs[0].Outcome != SideUp {
		t.Fatalf("expected one A/Up signal, got %+v", sigs)
	}
}

func TestStrategyARejectsNoEdge(t *testing.T) {
	e := New(config.StrategyAConfig{
		Enabled:        true,
		PriceMinDec:    decimal.RequireFromString("0.60"),
		PriceMaxDec:    decimal.RequireFromString("0.70"),
		MinEdgeDec:     decimal.RequireFromString("0.03"),
		SizeMinUSDDec:  decimal.RequireFromString("1"),
		SizeMaxUSDDec:  decimal.RequireFromString("3"),
		MinSecondsLeft: 60,
	}, config.StrategyBConfig{Enabled: false})

	in := MarketInput{
		UpTokenID:   "up",
		MidUp:       decimal.RequireFromString("0.65"),
		EdgeUp:      decimal.RequireFromString("0.01"),
		SecondsLeft: 120,
	}
	if sigs := e.Evaluate(in); len(sigs) != 0 {
		t.Fatalf("expected no signal, got %+v", sigs)
	}
}

func TestStrategyBOnSpotMove(t *testing.T) {
	e := New(config.StrategyAConfig{Enabled: false}, config.StrategyBConfig{
		Enabled:               true,
		PriceMoveThresholdDec: decimal.RequireFromString("0.002"),
		MaxSizeUSDDec:         decimal.RequireFromString("5"),
		MinEdgeDec:            decimal.RequireFromString("0.04"),
		MaxMarketPriceDec:     decimal.RequireFromString("0.75"),
		MinSecondsLeft:        30,
		LimitPriceMode:        "mid",
	})

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
	sigs := e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Strategy != "B" || sigs[0].Outcome != SideUp {
		t.Fatalf("expected B/Up, got %+v", sigs)
	}
}
