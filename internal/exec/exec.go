package exec

import (
	"fmt"
	"math"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/domain"
	"github.com/Gary05CX/polymarket/internal/store"
)

func BuyMinSize(cfg config.Execution, mode string, snap domain.MarketSnapshot, sig *domain.Signal, runID string) (domain.Order, domain.Fill, *domain.Reject, error) {
	if mode == "live" {
		return domain.Order{}, domain.Fill{}, nil, fmt.Errorf("live not implemented")
	}
	minShares := snap.Market.MinOrderSize
	if minShares <= 0 {
		minShares = snap.Book.MinOrderSize
	}
	if minShares <= 0 {
		minShares = 5
	}
	tick := snap.Market.TickSize
	if tick <= 0 {
		tick = snap.Book.TickSize
	}
	if tick <= 0 {
		tick = 0.001
	}
	feeRate := snap.Market.FeeRate
	limit := roundTick(sig.PMPrice+cfg.PriceOffset, tick)
	if limit >= 1 {
		limit = 1 - tick
	}
	cost := minShares * limit
	if cost > cfg.BudgetUSDCCap {
		return domain.Order{}, domain.Fill{}, &domain.Reject{
			ReasonCode: "budget", Reason: fmt.Sprintf("cost %.2f > cap %.2f", cost, cfg.BudgetUSDCCap),
			Slug: sig.MarketSlug, Features: map[string]any{"cost": cost},
		}, nil
	}
	avg, filled, ok := walkAsks(snap.Book.Asks, minShares)
	if !ok || filled+1e-9 < minShares {
		return domain.Order{}, domain.Fill{}, &domain.Reject{
			ReasonCode: "book", Reason: "not enough ask size for min shares",
			Slug: sig.MarketSlug,
		}, nil
	}
	fee := minShares * feeRate * avg * (1 - avg)
	o := domain.Order{
		ID: store.NewID(), RunID: runID, StrategyID: sig.StrategyID, StrategySource: sig.StrategySource,
		RunMode: mode, MarketSlug: sig.MarketSlug, Side: sig.Side, TokenID: sig.TokenID,
		IntendedPrice: sig.PMPrice, IntendedShares: minShares, LimitPrice: limit,
		NotionalUSDC: cost, IsMinSize: true, Status: "simulated",
	}
	f := domain.Fill{
		ID: store.NewID(), OrderID: o.ID, RunID: runID, MarketSlug: sig.MarketSlug,
		FillModel: "book_walk", Price: avg, Shares: minShares, FeeUSDC: fee, FeeRate: feeRate,
		LiquidityFlag: "taker",
	}
	return o, f, nil, nil
}

func walkAsks(asks []domain.BookLevel, shares float64) (avg, filled float64, ok bool) {
	need := shares
	notional := 0.0
	for _, a := range asks {
		if need <= 0 {
			break
		}
		take := math.Min(need, a.Size)
		notional += take * a.Price
		need -= take
	}
	filled = shares - need
	if filled <= 0 {
		return 0, 0, false
	}
	return notional / filled, filled, need <= 1e-9
}

func roundTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	return math.Round(px/tick) * tick
}
