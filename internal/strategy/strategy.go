// Package strategy implements Strategy A (stable small), B (lag harvest), and C (70% momentum).
package strategy

import (
	"fmt"
	"sync"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

// Skip is a near-miss / filtered opportunity (for observability).
type Skip struct {
	Strategy   string
	MarketSlug string
	Reason     string
}

// Engine evaluates A + B + C.
type Engine struct {
	A  config.StrategyAConfig
	B  config.StrategyBConfig
	C  config.StrategyCConfig
	FV config.FairValueConfig

	// Strategy C runtime: distance history, retrace watch, sustain clock, loss cooldown.
	cMu                sync.Mutex
	cHist              map[string][]distSample // slug -> recent abs(spot-open)
	cWatch             map[string]cWatch
	cSustain           map[string]cSustain // |move| held above thr after min elapsed
	cConsecutiveLosses int
	cCooldownUntil       time.Time
}

// New creates a strategy engine (fair_value bounds used for clamp filters).
func New(a config.StrategyAConfig, b config.StrategyBConfig, c config.StrategyCConfig, fv config.FairValueConfig) *Engine {
	return &Engine{
		A: a, B: b, C: c, FV: fv,
		cHist:    map[string][]distSample{},
		cWatch:   map[string]cWatch{},
		cSustain: map[string]cSustain{},
	}
}

// Evaluate returns signals and skip reasons (B near-misses, A clamp rejects when useful).
func (e *Engine) Evaluate(in MarketInput) (signals []Signal, skips []Skip) {
	if e.A.Enabled {
		sigs, sk := e.evalA(in)
		signals = append(signals, sigs...)
		skips = append(skips, sk...)
	}
	// A/B/C may all fire on the same market when one_order_per_market=false
	// (compare mode). Only collapse duplicate strategy+token in the same tick.
	seen := map[string]bool{} // strategy|token
	for _, s := range signals {
		seen[s.Strategy+"|"+s.TokenID] = true
	}
	if e.B.Enabled {
		sigs, sk := e.evalB(in)
		for _, s := range sigs {
			key := s.Strategy + "|" + s.TokenID
			if seen[key] {
				skips = append(skips, Skip{Strategy: "B", MarketSlug: in.Slug, Reason: "duplicate_in_tick"})
				continue
			}
			seen[key] = true
			signals = append(signals, s)
		}
		skips = append(skips, sk...)
	}
	if e.C.Enabled {
		sigs, sk := e.evalC(in)
		for _, s := range sigs {
			key := s.Strategy + "|" + s.TokenID
			if seen[key] {
				skips = append(skips, Skip{Strategy: "C", MarketSlug: in.Slug, Reason: "duplicate_in_tick"})
				continue
			}
			seen[key] = true
			signals = append(signals, s)
		}
		skips = append(skips, sk...)
	}
	return signals, skips
}

func (e *Engine) evalA(in MarketInput) ([]Signal, []Skip) {
	if in.SecondsLeft < float64(e.A.MinSecondsLeft) {
		return nil, nil
	}
	var skips []Skip
	var best *Signal

	try := func(outcome Side, tokenID string, mid, fair, edge, bestBid, bestAsk decimal.Decimal) {
		s, skip, ok := e.signalA(in, outcome, tokenID, mid, fair, edge, bestBid, bestAsk)
		if skip != nil {
			skips = append(skips, *skip)
		}
		if !ok {
			return
		}
		if best == nil || s.Edge.GreaterThan(best.Edge) {
			cp := s
			best = &cp
		}
	}

	try(SideUp, in.UpTokenID, in.MidUp, in.FairUp, in.EdgeUp, in.BestBidUp, in.BestAskUp)
	try(SideDown, in.DownTokenID, in.MidDown, in.FairDown, in.EdgeDown, in.BestBidDown, in.BestAskDown)

	if best == nil {
		return nil, skips
	}
	return []Signal{*best}, skips
}

func (e *Engine) signalA(
	in MarketInput,
	outcome Side,
	tokenID string,
	mid, fair, edge, bestBid, bestAsk decimal.Decimal,
) (Signal, *Skip, bool) {
	mkSkip := func(reason string) *Skip {
		return &Skip{Strategy: "A", MarketSlug: in.Slug, Reason: reason}
	}
	if tokenID == "" || mid.IsZero() {
		return Signal{}, nil, false
	}
	if mid.LessThan(e.A.PriceMinDec) || mid.GreaterThan(e.A.PriceMaxDec) {
		return Signal{}, nil, false // common; don't spam skips
	}

	// Reject fair pinned at model bounds (inflated edges).
	if e.A.RejectFairAtClamp && fairPinned(fair, e.FV.PMinDec, e.FV.PMaxDec) {
		return Signal{}, mkSkip(fmt.Sprintf("%s_fair_at_clamp fair=%s", outcome, fair)), false
	}

	// Dynamic min edge: higher mid needs more edge (worse win/lose asymmetry).
	reqEdge := requiredEdgeA(e.A, mid)
	if edge.LessThan(reqEdge) {
		return Signal{}, nil, false
	}
	// Cap absurd edges (usually clamp artifacts that slipped through).
	if !e.A.MaxEdgeDec.IsZero() && edge.GreaterThan(e.A.MaxEdgeDec) {
		return Signal{}, mkSkip(fmt.Sprintf("%s_edge_too_high edge=%s max=%s", outcome, edge, e.A.MaxEdgeDec)), false
	}

	sizeUSD := e.A.SizeMinUSDDec.Add(e.A.SizeMaxUSDDec).Div(decimal.NewFromInt(2))
	if sizeUSD.IsZero() {
		sizeUSD = e.A.SizeMinUSDDec
	}
	// Scale size down slightly as mid rises (optional soft de-risk).
	sizeUSD = scaleSizeByMid(sizeUSD, e.A.SizeMinUSDDec, mid, e.A.PriceMinDec, e.A.PriceMaxDec)

	price := limitPrice(e.A.LimitPriceMode, mid, bestBid, bestAsk, false)
	if price.IsZero() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return Signal{}, nil, false
	}
	size := sizeUSD.Div(price)
	return Signal{
		Strategy:    "A",
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
		Fair:        fair,
		Edge:        edge,
		SpotMove:    safeMove(in.OpenPrice, in.Spot),
		Reason:      fmt.Sprintf("A mid=%s fair=%s edge=%s req=%s band=[%s,%s]", mid, fair, edge, reqEdge, e.A.PriceMinDec, e.A.PriceMaxDec),
		SecondsLeft: in.SecondsLeft,
	}, nil, true
}

func requiredEdgeA(a config.StrategyAConfig, mid decimal.Decimal) decimal.Decimal {
	req := a.MinEdgeDec
	if a.EdgeSlopeDec.IsPositive() && mid.GreaterThan(a.PriceMinDec) {
		extra := mid.Sub(a.PriceMinDec).Mul(a.EdgeSlopeDec)
		req = req.Add(extra)
	}
	return req
}

// scaleSizeByMid: at price_min use sizeMax-ish mid of range; toward price_max shrink toward sizeMin.
func scaleSizeByMid(base, sizeMin, mid, pMin, pMax decimal.Decimal) decimal.Decimal {
	if pMax.LessThanOrEqual(pMin) || base.IsZero() {
		return base
	}
	// t=0 at pMin, t=1 at pMax
	t := mid.Sub(pMin).Div(pMax.Sub(pMin))
	if t.IsNegative() {
		t = decimal.Zero
	}
	if t.GreaterThan(decimal.NewFromInt(1)) {
		t = decimal.NewFromInt(1)
	}
	// interpolate base -> sizeMin as t goes 0->1
	// size = base*(1-t) + sizeMin*t
	return base.Mul(decimal.NewFromInt(1).Sub(t)).Add(sizeMin.Mul(t)).Round(4)
}

func (e *Engine) evalB(in MarketInput) ([]Signal, []Skip) {
	var skips []Skip
	add := func(reason string) {
		skips = append(skips, Skip{Strategy: "B", MarketSlug: in.Slug, Reason: reason})
	}

	if in.SecondsLeft < float64(e.B.MinSecondsLeft) {
		return nil, nil // too common near expiry
	}
	if in.OpenPrice.IsZero() || in.Spot.IsZero() {
		add("no_open_or_spot")
		return nil, skips
	}
	move := safeMove(in.OpenPrice, in.Spot)
	absMove := move.Abs()
	if absMove.LessThan(e.B.PriceMoveThresholdDec) {
		return nil, nil // not a candidate
	}

	// We have a real move — log why we skip if we don't signal.
	if move.IsPositive() {
		if s, skip, ok := e.signalB(in, SideUp, in.UpTokenID, in.MidUp, in.FairUp, in.EdgeUp, in.BestBidUp, in.BestAskUp, move); ok {
			return []Signal{s}, skips
		} else if skip != "" {
			add(skip)
		}
	} else if move.IsNegative() {
		if s, skip, ok := e.signalB(in, SideDown, in.DownTokenID, in.MidDown, in.FairDown, in.EdgeDown, in.BestBidDown, in.BestAskDown, move); ok {
			return []Signal{s}, skips
		} else if skip != "" {
			add(skip)
		}
	}
	return nil, skips
}

func (e *Engine) signalB(
	in MarketInput,
	outcome Side,
	tokenID string,
	mid, fair, edge, bestBid, bestAsk, move decimal.Decimal,
) (Signal, string, bool) {
	if tokenID == "" || mid.IsZero() {
		return Signal{}, "missing_token_or_mid", false
	}
	if e.B.RejectFairAtClamp && fairPinned(fair, e.FV.PMinDec, e.FV.PMaxDec) {
		return Signal{}, fmt.Sprintf("fair_at_clamp fair=%s", fair), false
	}
	if mid.GreaterThan(e.B.MaxMarketPriceDec) {
		return Signal{}, fmt.Sprintf("mid_too_high mid=%s max=%s", mid, e.B.MaxMarketPriceDec), false
	}
	if edge.LessThan(e.B.MinEdgeDec) {
		return Signal{}, fmt.Sprintf("edge_low edge=%s min=%s", edge, e.B.MinEdgeDec), false
	}
	sizeUSD := e.B.MaxSizeUSDDec
	price := limitPrice(e.B.LimitPriceMode, mid, bestBid, bestAsk, true)
	if price.IsZero() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return Signal{}, "bad_limit_price", false
	}
	size := sizeUSD.Div(price)
	return Signal{
		Strategy:    "B",
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
		Fair:        fair,
		Edge:        edge,
		SpotMove:    move,
		Reason:      fmt.Sprintf("B move=%s mid=%s fair=%s edge=%s thr=%s", move, mid, fair, edge, e.B.PriceMoveThresholdDec),
		SecondsLeft: in.SecondsLeft,
	}, "", true
}

func fairPinned(fair, pMin, pMax decimal.Decimal) bool {
	if fair.IsZero() {
		return false
	}
	// small epsilon via 1e-6
	eps := decimal.NewFromFloat(1e-6)
	if !pMin.IsZero() && fair.LessThanOrEqual(pMin.Add(eps)) {
		return true
	}
	if !pMax.IsZero() && fair.GreaterThanOrEqual(pMax.Sub(eps)) {
		return true
	}
	return false
}

func limitPrice(mode string, mid, bestBid, bestAsk decimal.Decimal, aggressive bool) decimal.Decimal {
	switch mode {
	case "best_bid":
		if !bestBid.IsZero() {
			return bestBid
		}
		return mid
	case "best_ask":
		if !bestAsk.IsZero() {
			return bestAsk
		}
		return mid
	case "mid":
		fallthrough
	default:
		if aggressive && !bestAsk.IsZero() {
			if !mid.IsZero() {
				return mid.Add(bestAsk).Div(decimal.NewFromInt(2))
			}
			return bestAsk
		}
		if !mid.IsZero() {
			return mid
		}
		if !bestBid.IsZero() {
			return bestBid
		}
		return bestAsk
	}
}

func safeMove(open, spot decimal.Decimal) decimal.Decimal {
	if open.IsZero() {
		return decimal.Zero
	}
	return spot.Sub(open).Div(open)
}
