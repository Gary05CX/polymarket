// Package strategy implements Strategy A (stable small) and Strategy B (lag harvest).
package strategy

import (
	"fmt"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

// Engine evaluates A + B.
type Engine struct {
	A config.StrategyAConfig
	B config.StrategyBConfig
}

// New creates a strategy engine.
func New(a config.StrategyAConfig, b config.StrategyBConfig) *Engine {
	return &Engine{A: a, B: b}
}

// Evaluate returns zero or more non-conflicting signals (A preferred over B per side).
func (e *Engine) Evaluate(in MarketInput) []Signal {
	var out []Signal
	if e.A.Enabled {
		out = append(out, e.evalA(in)...)
	}
	if e.B.Enabled {
		// avoid double-buying same token in same tick if A already signaled
		have := map[string]bool{}
		for _, s := range out {
			have[s.TokenID] = true
		}
		for _, s := range e.evalB(in) {
			if !have[s.TokenID] {
				out = append(out, s)
			}
		}
	}
	return out
}

func (e *Engine) evalA(in MarketInput) []Signal {
	if in.SecondsLeft < float64(e.A.MinSecondsLeft) {
		return nil
	}
	var signals []Signal
	// Check Up
	if s, ok := e.signalA(in, SideUp, in.UpTokenID, in.MidUp, in.FairUp, in.EdgeUp, in.BestBidUp, in.BestAskUp); ok {
		signals = append(signals, s)
	}
	// Check Down
	if s, ok := e.signalA(in, SideDown, in.DownTokenID, in.MidDown, in.FairDown, in.EdgeDown, in.BestBidDown, in.BestAskDown); ok {
		signals = append(signals, s)
	}
	return signals
}

func (e *Engine) signalA(
	in MarketInput,
	outcome Side,
	tokenID string,
	mid, fair, edge, bestBid, bestAsk decimal.Decimal,
) (Signal, bool) {
	if tokenID == "" || mid.IsZero() {
		return Signal{}, false
	}
	if mid.LessThan(e.A.PriceMinDec) || mid.GreaterThan(e.A.PriceMaxDec) {
		return Signal{}, false
	}
	if edge.LessThan(e.A.MinEdgeDec) {
		return Signal{}, false
	}
	sizeUSD := e.A.SizeMinUSDDec.Add(e.A.SizeMaxUSDDec).Div(decimal.NewFromInt(2))
	if sizeUSD.IsZero() {
		sizeUSD = e.A.SizeMinUSDDec
	}
	price := limitPrice(e.A.LimitPriceMode, mid, bestBid, bestAsk, false)
	if price.IsZero() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return Signal{}, false
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
		Fair:        fair,
		Edge:        edge,
		SpotMove:    safeMove(in.OpenPrice, in.Spot),
		Reason:      fmt.Sprintf("A mid=%s fair=%s edge=%s band=[%s,%s]", mid, fair, edge, e.A.PriceMinDec, e.A.PriceMaxDec),
		SecondsLeft: in.SecondsLeft,
	}, true
}

func (e *Engine) evalB(in MarketInput) []Signal {
	if in.SecondsLeft < float64(e.B.MinSecondsLeft) {
		return nil
	}
	if in.OpenPrice.IsZero() || in.Spot.IsZero() {
		return nil
	}
	move := safeMove(in.OpenPrice, in.Spot)
	absMove := move.Abs()
	if absMove.LessThan(e.B.PriceMoveThresholdDec) {
		return nil
	}

	// Spot moved up → prefer Up token if market still lagging
	if move.IsPositive() {
		if s, ok := e.signalB(in, SideUp, in.UpTokenID, in.MidUp, in.FairUp, in.EdgeUp, in.BestBidUp, in.BestAskUp, move); ok {
			return []Signal{s}
		}
	}
	// Spot moved down → prefer Down
	if move.IsNegative() {
		if s, ok := e.signalB(in, SideDown, in.DownTokenID, in.MidDown, in.FairDown, in.EdgeDown, in.BestBidDown, in.BestAskDown, move); ok {
			return []Signal{s}
		}
	}
	return nil
}

func (e *Engine) signalB(
	in MarketInput,
	outcome Side,
	tokenID string,
	mid, fair, edge, bestBid, bestAsk, move decimal.Decimal,
) (Signal, bool) {
	if tokenID == "" || mid.IsZero() {
		return Signal{}, false
	}
	// market still "cheap" relative to fair and not already expensive
	if mid.GreaterThan(e.B.MaxMarketPriceDec) {
		return Signal{}, false
	}
	if edge.LessThan(e.B.MinEdgeDec) {
		return Signal{}, false
	}
	sizeUSD := e.B.MaxSizeUSDDec
	price := limitPrice(e.B.LimitPriceMode, mid, bestBid, bestAsk, true)
	if price.IsZero() || price.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return Signal{}, false
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
		Fair:        fair,
		Edge:        edge,
		SpotMove:    move,
		Reason:      fmt.Sprintf("B move=%s mid=%s fair=%s edge=%s thr=%s", move, mid, fair, edge, e.B.PriceMoveThresholdDec),
		SecondsLeft: in.SecondsLeft,
	}, true
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
			// slightly aggressive: halfway mid→ask
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
