package strategy

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// distSample is a signed distance of spot from window open (target).
type distSample struct {
	t   time.Time
	abs decimal.Decimal
}

// cWatch tracks "wait for stability after retrace" per market.
type cWatch struct {
	started   time.Time
	deadline  time.Time
	direction int // +1 up leading, -1 down leading
}

// evalC implements the manual 70% momentum rules + retrace wait.
func (e *Engine) evalC(in MarketInput) ([]Signal, []Skip) {
	var skips []Skip
	add := func(reason string) {
		skips = append(skips, Skip{Strategy: "C", MarketSlug: in.Slug, Reason: reason})
	}

	if e.C.SizeUSDDec.IsZero() {
		return nil, nil
	}

	now := time.Now().UTC()
	if e.cCooldownUntil.After(now) {
		add(fmt.Sprintf("cooldown_until=%s", e.cCooldownUntil.Format(time.RFC3339)))
		return nil, skips
	}

	if in.SecondsLeft < float64(e.C.MinSecondsLeft) {
		return nil, nil
	}
	if in.ElapsedSec < float64(e.C.MinElapsedSec) {
		return nil, nil // too early in window — common
	}
	if in.OpenPrice.IsZero() || in.Spot.IsZero() {
		add("no_open_or_spot")
		return nil, skips
	}

	thr := e.moveThreshold(in.Asset)
	if thr.IsZero() {
		add("no_move_threshold_for_asset")
		return nil, skips
	}

	signed := in.Spot.Sub(in.OpenPrice)
	absMove := signed.Abs()
	e.recordDist(in.Slug, now, absMove)

	if absMove.LessThan(thr) {
		e.clearWatch(in.Slug)
		return nil, nil // not far enough
	}

	// Leading side from spot vs open.
	var outcome Side
	var tokenID string
	var mid, bestBid, bestAsk decimal.Decimal
	dir := 0
	if signed.IsPositive() {
		outcome, tokenID = SideUp, in.UpTokenID
		mid, bestBid, bestAsk = in.MidUp, in.BestBidUp, in.BestAskUp
		dir = 1
	} else if signed.IsNegative() {
		outcome, tokenID = SideDown, in.DownTokenID
		mid, bestBid, bestAsk = in.MidDown, in.BestBidDown, in.BestAskDown
		dir = -1
	} else {
		return nil, nil
	}
	if tokenID == "" || mid.IsZero() {
		add("missing_token_or_mid")
		return nil, skips
	}
	if mid.LessThan(e.C.MidMinDec) || mid.GreaterThan(e.C.MidMaxDec) {
		// Common when market already at 90%+ or still cheap — don't spam.
		return nil, nil
	}

	// Retrace / stability gate (user: 線往 target 靠近就等 30s–1m，不穩不下).
	retracing := e.isRetracing(in.Slug, absMove, now)
	if retracing {
		w, ok := e.getWatch(in.Slug)
		if !ok || w.direction != dir {
			e.startWatch(in.Slug, dir, now)
			add(fmt.Sprintf("retrace_wait started=%ds", e.C.StabilityWaitSec))
			return nil, skips
		}
		if now.Before(w.deadline) {
			add(fmt.Sprintf("retrace_waiting remaining=%.0fs", w.deadline.Sub(now).Seconds()))
			return nil, skips
		}
		// Wait finished — still retracing ⇒ skip this setup.
		if e.isRetracing(in.Slug, absMove, now) {
			add("retrace_unstable_after_wait")
			return nil, skips
		}
		// Stabilized after wait — allow through.
		e.clearWatch(in.Slug)
	} else {
		e.clearWatch(in.Slug)
	}

	price := limitPrice(e.C.LimitPriceMode, mid, bestBid, bestAsk, false)
	if price.IsZero() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		add("bad_limit_price")
		return nil, skips
	}
	sizeUSD := e.C.SizeUSDDec
	size := sizeUSD.Div(price)
	// Soft min shares for CLOB (live later); paper still records.
	sig := Signal{
		Strategy:    "C",
		MarketSlug:  in.Slug,
		Asset:       in.Asset,
		Timeframe:   in.Timeframe,
		TokenID:     tokenID,
		Outcome:     outcome,
		Price:       price,
		SizeUSD:     sizeUSD,
		Size:        size,
		MarketMid:   mid,
		BestBid:     bestBid,
		BestAsk:     bestAsk,
		SpotMove:    signed,
		Reason: fmt.Sprintf(
			"C abs_move=%s thr=%s mid=%s elapsed=%.0f band=[%s,%s]",
			absMove.StringFixed(4), thr.String(), mid.String(), in.ElapsedSec,
			e.C.MidMinDec.String(), e.C.MidMaxDec.String(),
		),
		SecondsLeft: in.SecondsLeft,
	}
	return []Signal{sig}, skips
}

func (e *Engine) moveThreshold(asset string) decimal.Decimal {
	switch strings.ToLower(strings.TrimSpace(asset)) {
	case "btc", "bitcoin":
		return e.C.BTCMoveUSDDec
	case "eth", "ethereum":
		return e.C.ETHMoveUSDDec
	default:
		return decimal.Zero
	}
}

func (e *Engine) recordDist(slug string, now time.Time, abs decimal.Decimal) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if e.cHist == nil {
		e.cHist = map[string][]distSample{}
	}
	s := e.cHist[slug]
	s = append(s, distSample{t: now, abs: abs})
	// Keep ~3 minutes of history.
	cut := now.Add(-3 * time.Minute)
	i := 0
	for i < len(s) && s[i].t.Before(cut) {
		i++
	}
	if i > 0 {
		s = s[i:]
	}
	e.cHist[slug] = s
}

// isRetracing: abs distance from target shrank vs lookback window (moving back toward open).
func (e *Engine) isRetracing(slug string, absNow decimal.Decimal, now time.Time) bool {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	s := e.cHist[slug]
	if len(s) < 2 {
		return false
	}
	lookback := time.Duration(e.C.StabilityLookbackSec) * time.Second
	if lookback <= 0 {
		lookback = 30 * time.Second
	}
	targetT := now.Add(-lookback)
	// Oldest sample at or after targetT; if none, use oldest.
	var past *distSample
	for i := range s {
		if !s[i].t.After(targetT) {
			past = &s[i]
		}
	}
	if past == nil {
		past = &s[0]
	}
	// Need enough time separation.
	if now.Sub(past.t) < lookback/2 {
		return false
	}
	eps := e.C.RetraceEpsilonUSDDec
	// Retracing if abs move fell by more than epsilon.
	return absNow.LessThan(past.abs.Sub(eps))
}

func (e *Engine) startWatch(slug string, dir int, now time.Time) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if e.cWatch == nil {
		e.cWatch = map[string]cWatch{}
	}
	wait := time.Duration(e.C.StabilityWaitSec) * time.Second
	if wait <= 0 {
		wait = 45 * time.Second
	}
	e.cWatch[slug] = cWatch{started: now, deadline: now.Add(wait), direction: dir}
}

func (e *Engine) getWatch(slug string) (cWatch, bool) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	w, ok := e.cWatch[slug]
	return w, ok
}

func (e *Engine) clearWatch(slug string) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if e.cWatch != nil {
		delete(e.cWatch, slug)
	}
}

// RecordCOutcome updates consecutive-loss cooldown after a Strategy C settlement.
func (e *Engine) RecordCOutcome(pnlUSD decimal.Decimal) {
	if e == nil || !e.C.Enabled {
		return
	}
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if pnlUSD.IsPositive() {
		e.cConsecutiveLosses = 0
		return
	}
	if pnlUSD.IsZero() {
		return
	}
	e.cConsecutiveLosses++
	maxL := e.C.MaxConsecutiveLosses
	if maxL <= 0 {
		maxL = 2
	}
	if e.cConsecutiveLosses >= maxL {
		cd := time.Duration(e.C.CooldownSec) * time.Second
		if cd <= 0 {
			cd = 45 * time.Minute
		}
		e.cCooldownUntil = time.Now().UTC().Add(cd)
		e.cConsecutiveLosses = 0
	}
}

// CCooldownUntil exposes cooldown end for logging/tests.
func (e *Engine) CCooldownUntil() time.Time {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	return e.cCooldownUntil
}
