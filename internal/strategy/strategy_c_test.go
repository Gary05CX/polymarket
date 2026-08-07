package strategy

import (
	"testing"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

func testC() config.StrategyCConfig {
	return config.StrategyCConfig{
		Enabled:              true,
		SizeUSDDec:           decimal.RequireFromString("5"),
		MinElapsedSec:        60,
		SustainedAboveSec:    2, // short for tests
		BTCMoveUSDDec:        decimal.RequireFromString("2"),
		ETHMoveUSDDec:        decimal.RequireFromString("0.2"),
		MidMinDec:            decimal.RequireFromString("0.55"),
		MidMaxDec:            decimal.RequireFromString("0.88"),
		MinSecondsLeft:       20,
		LimitPriceMode:       "mid",
		StabilityWaitSec:     2,
		StabilityLookbackSec: 2,
		RetraceEpsilonUSDDec: decimal.RequireFromString("0.15"),
		MaxConsecutiveLosses: 2,
		CooldownSec:            60,
	}
}

func TestStrategyCTriggersAfterSustain(t *testing.T) {
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		testC(),
		testFV(),
	)
	slug := "btc-updown-5m-c"
	// Growing abs move (not retracing).
	e.recordDist(slug, time.Now().UTC().Add(-3*time.Second), decimal.RequireFromString("2.5"))
	e.recordDist(slug, time.Now().UTC(), decimal.RequireFromString("3.0"))

	in := MarketInput{
		Slug:        slug,
		Asset:       "btc",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("65000"),
		Spot:        decimal.RequireFromString("65003"), // +3 > 2
		MidUp:       decimal.RequireFromString("0.70"),
		MidDown:     decimal.RequireFromString("0.30"),
		ElapsedSec:  90,
		SecondsLeft: 150,
	}
	// First tick: starts sustain, no signal yet.
	sigs, skips := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("first tick should wait sustain, got %+v", sigs)
	}
	if len(skips) == 0 {
		t.Fatal("want sustain_waiting skip")
	}
	time.Sleep(2100 * time.Millisecond)
	e.recordDist(slug, time.Now().UTC(), decimal.RequireFromString("3.1"))
	in.Spot = decimal.RequireFromString("65003.1")
	sigs, _ = e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Strategy != "C" || sigs[0].Outcome != SideUp {
		t.Fatalf("want C Up after sustain, got %+v", sigs)
	}
}

func TestStrategyCSkipsBeforeOneMinute(t *testing.T) {
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		testC(),
		testFV(),
	)
	in := MarketInput{
		Slug:        "btc-early",
		Asset:       "btc",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("65000"),
		Spot:        decimal.RequireFromString("65010"),
		MidUp:       decimal.RequireFromString("0.70"),
		ElapsedSec:  30, // < 60
		SecondsLeft: 270,
	}
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("too early, got %+v", sigs)
	}
}

func TestStrategyCETHThreshold(t *testing.T) {
	cfg := testC()
	cfg.SustainedAboveSec = 1
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		cfg,
		testFV(),
	)
	slug := "eth-c"
	e.recordDist(slug, time.Now().UTC().Add(-2*time.Second), decimal.RequireFromString("0.25"))
	in := MarketInput{
		Slug:        slug,
		Asset:       "eth",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("2000"),
		Spot:        decimal.RequireFromString("1999.7"), // abs 0.3 > 0.2 Down
		MidUp:       decimal.RequireFromString("0.35"),
		MidDown:     decimal.RequireFromString("0.65"),
		ElapsedSec:  90,
		SecondsLeft: 150,
	}
	e.Evaluate(in)
	time.Sleep(1100 * time.Millisecond)
	e.recordDist(slug, time.Now().UTC(), decimal.RequireFromString("0.35"))
	in.Spot = decimal.RequireFromString("1999.65")
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 1 || sigs[0].Outcome != SideDown {
		t.Fatalf("want ETH Down, got %+v", sigs)
	}
}

func TestStrategyCCooldownAfterTwoLosses(t *testing.T) {
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		testC(),
		testFV(),
	)
	e.RecordCOutcome(decimal.RequireFromString("-5"))
	e.RecordCOutcome(decimal.RequireFromString("-5"))
	if e.CCooldownUntil().IsZero() || !e.CCooldownUntil().After(time.Now()) {
		t.Fatalf("expected cooldown, until=%v", e.CCooldownUntil())
	}
}
