// Package settle resolves expired windows and records paper (or approximate) PnL.
package settle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/shopspring/decimal"
)

// Result is one market settlement.
type Result struct {
	Slug      string
	Outcome   string // "Up" | "Down" | "Unknown"
	PnLUSD    decimal.Decimal
	FeeUSD    decimal.Decimal
	Open      decimal.Decimal
	Close     decimal.Decimal
	Positions int
}

// Engine settles expired markets using open vs close spot (paper resolve).
// Polymarket official resolve is Chainlink; this is an approximation for dry-run analytics.
type Engine struct {
	st     *store.Store
	log    *slog.Logger
	feeBps int
}

// New creates a settlement engine. feeBps is applied to notional cost on paper fills.
func New(st *store.Store, log *slog.Logger, feeBps int) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if feeBps < 0 {
		feeBps = 0
	}
	return &Engine{st: st, log: log, feeBps: feeBps}
}

// RunSettlements settles all due markets. spotByAsset is live feed fallback if no close_price.
func (e *Engine) RunSettlements(ctx context.Context, now time.Time, spotByAsset map[string]decimal.Decimal) ([]Result, error) {
	due, err := e.st.ListMarketsDueForSettle(ctx, now)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, m := range due {
		r, err := e.settleOne(ctx, m, now, spotByAsset)
		if err != nil {
			e.log.Warn("settle failed", "slug", m.Slug, "err", err)
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (e *Engine) settleOne(ctx context.Context, m store.Market, now time.Time, spotByAsset map[string]decimal.Decimal) (Result, error) {
	r := Result{Slug: m.Slug}

	open := decimal.Zero
	if m.OpenPrice != nil {
		if d, err := decimal.NewFromString(*m.OpenPrice); err == nil {
			open = d
		}
	}

	// Prefer locked close_price, then last snapshot, then live feed.
	closePx, ok, err := e.st.GetClosePrice(ctx, m.Slug)
	if err != nil {
		return r, err
	}
	if !ok || closePx.IsZero() {
		closePx, ok, err = e.st.LatestSpotForMarket(ctx, m.Slug)
		if err != nil {
			return r, err
		}
	}
	if (!ok || closePx.IsZero()) && spotByAsset != nil {
		if s, ok2 := spotByAsset[m.Asset]; ok2 && !s.IsZero() {
			closePx = s
			ok = true
		}
	}
	r.Open = open
	r.Close = closePx

	outcome := "Unknown"
	if ok && !open.IsZero() && !closePx.IsZero() {
		if closePx.GreaterThanOrEqual(open) {
			outcome = "Up"
		} else {
			outcome = "Down"
		}
	}
	r.Outcome = outcome

	positions, err := e.st.ListPositions(ctx, m.Slug)
	if err != nil {
		return r, err
	}
	r.Positions = len(positions)

	if len(positions) == 0 {
		orders, err := e.st.ListOrdersForMarket(ctx, m.Slug)
		if err != nil {
			return r, err
		}
		for _, o := range orders {
			if o.Status != "dry_run" && o.Status != "dry_filled" && o.Status != "live" && o.Status != "pending" && o.Status != "open" {
				continue
			}
			positions = append(positions, store.Position{
				MarketSlug: o.MarketSlug,
				TokenID:    o.TokenID,
				Size:       o.Size,
				AvgPrice:   o.Price,
				SizeUSD:    o.SizeUSD,
			})
		}
		r.Positions = len(positions)
	}

	totalPnL := decimal.Zero
	totalFee := decimal.Zero
	winToken := ""
	switch outcome {
	case "Up":
		winToken = m.UpTokenID
	case "Down":
		winToken = m.DownTokenID
	}

	feeRate := decimal.NewFromInt(int64(e.feeBps)).Div(decimal.NewFromInt(10000))

	for _, p := range positions {
		size, _ := decimal.NewFromString(p.Size)
		avg, _ := decimal.NewFromString(p.AvgPrice)
		costUSD, _ := decimal.NewFromString(p.SizeUSD)
		if costUSD.IsZero() && !size.IsZero() && !avg.IsZero() {
			costUSD = size.Mul(avg)
		}
		fee := costUSD.Mul(feeRate)
		totalFee = totalFee.Add(fee)

		var pnl decimal.Decimal
		if outcome == "Unknown" || winToken == "" {
			// cannot resolve reliably — record 0 PnL, no fee
			totalFee = totalFee.Sub(fee)
			pnl = decimal.Zero
		} else if p.TokenID == winToken {
			// win: size * $1 - cost - fee
			pnl = size.Sub(costUSD).Sub(fee)
		} else {
			pnl = costUSD.Neg().Sub(fee)
		}
		totalPnL = totalPnL.Add(pnl)
	}
	r.PnLUSD = totalPnL
	r.FeeUSD = totalFee

	kind := "paper_settle"
	detail := fmt.Sprintf(
		`{"slug":%q,"outcome":%q,"open":%q,"close":%q,"positions":%d,"fee_usd":%q,"fee_bps":%d}`,
		m.Slug, outcome, open.String(), closePx.String(), r.Positions, totalFee.String(), e.feeBps,
	)
	if err := e.st.RecordPnL(ctx, kind, totalPnL, detail); err != nil {
		return r, err
	}
	if err := e.st.MarkOrdersStatus(ctx, m.Slug, "dry_settled", []string{
		"dry_run", "dry_filled", "pending", "live", "open",
	}); err != nil {
		return r, err
	}
	if err := e.st.ClearPositions(ctx, m.Slug); err != nil {
		return r, err
	}
	if err := e.st.MarkMarketSettled(ctx, m.Slug, outcome, totalPnL, now); err != nil {
		return r, err
	}

	e.log.Info("market settled",
		"slug", m.Slug,
		"outcome", outcome,
		"pnl_usd", totalPnL.String(),
		"fee_usd", totalFee.String(),
		"open", open.String(),
		"close", closePx.String(),
		"positions", r.Positions,
	)
	return r, nil
}
