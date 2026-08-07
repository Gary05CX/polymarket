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
		MinElapsedSec:        120,
		BTCMoveUSDDec:        decimal.RequireFromString("10"),
		ETHMoveUSDDec:        decimal.RequireFromString("1"),
		MidMinDec:            decimal.RequireFromString("0.65"),
		MidMaxDec:            decimal.RequireFromString("0.75"),
		MinSecondsLeft:       20,
		LimitPriceMode:       "mid",
		StabilityWaitSec:     2, // short for tests
		StabilityLookbackSec: 2,
		RetraceEpsilonUSDDec: decimal.RequireFromString("0.3"),
		MaxConsecutiveLosses: 2,
		CooldownSec:            60,
	}
}

func TestStrategyCTriggersOnBTCMove(t *testing.T) {
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		testC(),
		testFV(),
	)
	// Seed history so not "retracing" (abs flat/growing).
	slug := "btc-updown-5m-c"
	now := time.Now().UTC()
	e.recordDist(slug, now.Add(-3*time.Second), decimal.RequireFromString("9"))
	e.recordDist(slug, now.Add(-1*time.Second), decimal.RequireFromString("11"))

	in := MarketInput{
		Slug:        slug,
		Asset:       "btc",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("65000"),
		Spot:        decimal.RequireFromString("65012"), // +12 > 10
		MidUp:       decimal.RequireFromString("0.70"),
		MidDown:     decimal.RequireFromString("0.30"),
		ElapsedSec:  150,
		SecondsLeft: 150,
	}
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 1 {
		t.Fatalf("want 1 signal, got %d", len(sigs))
	}
	if sigs[0].Strategy != "C" || sigs[0].Outcome != SideUp {
		t.Fatalf("unexpected signal: %+v", sigs[0])
	}
	if !sigs[0].SizeUSD.Equal(decimal.RequireFromString("5")) {
		t.Fatalf("size %s", sigs[0].SizeUSD)
	}
}

func TestStrategyCSkipsWhenMidTooHigh(t *testing.T) {
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		testC(),
		testFV(),
	)
	in := MarketInput{
		Slug:        "btc-c2",
		Asset:       "btc",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("65000"),
		Spot:        decimal.RequireFromString("65020"),
		MidUp:       decimal.RequireFromString("0.92"),
		MidDown:     decimal.RequireFromString("0.08"),
		ElapsedSec:  150,
		SecondsLeft: 150,
	}
	sigs, _ := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("want skip high mid, got %+v", sigs)
	}
}

func TestStrategyCRetraceWaitThenUnstable(t *testing.T) {
	cfg := testC()
	cfg.StabilityWaitSec = 1
	cfg.StabilityLookbackSec = 2
	e := New(
		config.StrategyAConfig{Enabled: false},
		config.StrategyBConfig{Enabled: false},
		cfg,
		testFV(),
	)
	slug := "eth-c-retrace"
	// Past far, now closer to open → retracing.
	e.recordDist(slug, time.Now().UTC().Add(-3*time.Second), decimal.RequireFromString("3.0"))
	e.recordDist(slug, time.Now().UTC(), decimal.RequireFromString("1.2"))

	in := MarketInput{
		Slug:        slug,
		Asset:       "eth",
		UpTokenID:   "up",
		DownTokenID: "down",
		OpenPrice:   decimal.RequireFromString("2000"),
		Spot:        decimal.RequireFromString("2001.2"), // abs 1.2 >= 1 thr
		MidUp:       decimal.RequireFromString("0.70"),
		MidDown:     decimal.RequireFromString("0.30"),
		ElapsedSec:  150,
		SecondsLeft: 150,
	}
	sigs, skips := e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("should wait on retrace, got sigs %+v", sigs)
	}
	if len(skips) == 0 {
		t.Fatal("want retrace skip reason")
	}

	// Still retracing after wait deadline.
	time.Sleep(1100 * time.Millisecond)
	e.recordDist(slug, time.Now().UTC().Add(-3*time.Second), decimal.RequireFromString("3.0"))
	e.recordDist(slug, time.Now().UTC(), decimal.RequireFromString("1.1"))
	in.Spot = decimal.RequireFromString("2001.1")
	sigs, skips = e.Evaluate(in)
	if len(sigs) != 0 {
		t.Fatalf("still unstable, want no signal, got %+v skips=%v", sigs, skips)
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
