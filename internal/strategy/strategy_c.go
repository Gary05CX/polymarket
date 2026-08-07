package strategy

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// distSample is abs distance of spot from window open (target).
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

// cSustain tracks continuous time |spot-open| has stayed above threshold
// (only counted after MinElapsedSec into the window).
type cSustain struct {
	since time.Time
	dir   int
}

// evalC: after market open ≥1m, if |spot-open| stays above thr for ≥2m, buy $5 on leader.
// thr: BTC > $2, ETH > $0.2 (configurable). Optional mid band + retrace wait.
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

	minEl := e.C.MinElapsedSec
	if minEl <= 0 {
		minEl = 60
	}
	sustSec := e.C.SustainedAboveSec
	if sustSec <= 0 {
		sustSec = 120
	}

	// First minute of the market: do not start sustain clock.
	if in.ElapsedSec < float64(minEl) {
		e.clearSustain(in.Slug)
		e.clearWatch(in.Slug)
		return nil, nil
	}

	// Strictly greater than thr (user: 離 target > 2 / > 0.2).
	if !absMove.GreaterThan(thr) {
		e.clearSustain(in.Slug)
		e.clearWatch(in.Slug)
		return nil, nil
	}

	// Leading side from spot vs open ("跟大的那邊").
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
		e.clearSustain(in.Slug)
		return nil, nil
	}
	if tokenID == "" || mid.IsZero() {
		add("missing_token_or_mid")
		return nil, skips
	}

	// Continuous hold above thr for SustainedAboveSec (starts only after min elapsed).
	held, rem := e.touchSustain(in.Slug, dir, now, sustSec)
	if !held {
		add(fmt.Sprintf("sustain_waiting remaining=%.0fs need=%ds thr=%s abs=%s", rem, sustSec, thr.String(), absMove.StringFixed(4)))
		return nil, skips
	}

	if !e.C.MidMinDec.IsZero() || !e.C.MidMaxDec.IsZero() {
		if mid.LessThan(e.C.MidMinDec) || mid.GreaterThan(e.C.MidMaxDec) {
			return nil, nil // mid out of band — common, no spam
		}
	}

	// Retrace / stability gate (optional extra).
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
		if e.isRetracing(in.Slug, absMove, now) {
			add("retrace_unstable_after_wait")
			return nil, skips
		}
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
	sig := Signal{
		Strategy:   "C",
		MarketSlug: in.Slug,
		Asset:      in.Asset,
		Timeframe:  in.Timeframe,
		TokenID:    tokenID,
		Outcome:    outcome,
		Price:      price,
		SizeUSD:    sizeUSD,
		Size:       size,
		MarketMid:  mid,
		BestBid:    bestBid,
		BestAsk:    bestAsk,
		SpotMove:   signed,
		Reason: fmt.Sprintf(
			"C abs_move=%s thr=%s mid=%s elapsed=%.0f sustained=%ds band=[%s,%s]",
			absMove.StringFixed(4), thr.String(), mid.String(), in.ElapsedSec, sustSec,
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

// touchSustain returns (ready, remainingSeconds). Resets if direction flips or thr broken (caller clears).
func (e *Engine) touchSustain(slug string, dir int, now time.Time, needSec int) (bool, float64) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if e.cSustain == nil {
		e.cSustain = map[string]cSustain{}
	}
	s, ok := e.cSustain[slug]
	if !ok || s.dir != dir || s.since.IsZero() {
		e.cSustain[slug] = cSustain{since: now, dir: dir}
		return false, float64(needSec)
	}
	elapsed := now.Sub(s.since).Seconds()
	need := float64(needSec)
	if elapsed >= need {
		return true, 0
	}
	return false, need - elapsed
}

func (e *Engine) clearSustain(slug string) {
	e.cMu.Lock()
	defer e.cMu.Unlock()
	if e.cSustain != nil {
		delete(e.cSustain, slug)
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
	// Keep ~5 minutes (sustain window + buffer).
	cut := now.Add(-5 * time.Minute)
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
	var past *distSample
	for i := range s {
		if !s[i].t.After(targetT) {
			past = &s[i]
		}
	}
	if past == nil {
		past = &s[0]
	}
	if now.Sub(past.t) < lookback/2 {
		return false
	}
	eps := e.C.RetraceEpsilonUSDDec
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
