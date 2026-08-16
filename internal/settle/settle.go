// Package settle resolves expired windows and records paper (or approximate) PnL.
package settle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/shopspring/decimal"
)

// OfficialLookup returns the Polymarket/Chainlink winner when ready.
// ok=false means not resolved yet (skip this tick; do not use Binance).
type OfficialLookup func(ctx context.Context, slug string) (outcome string, ready bool, err error)

// ErrOfficialPending means Gamma has not published a resolved outcome yet.
var ErrOfficialPending = errors.New("official outcome not ready")

// Result is one market settlement.
type Result struct {
	Slug      string
	Outcome   string // "Up" | "Down" | "Unknown"
	PnLUSD    decimal.Decimal
	FeeUSD    decimal.Decimal
	Open      decimal.Decimal
	Close     decimal.Decimal
	Positions int
	// Kind is paper_settle or live_settle (matches pnl_ledger.kind).
	Kind     string
	Live     bool
	Official bool // true when outcome came from Gamma/Chainlink, not Binance
}

// Engine settles expired markets using official Gamma outcome when provided.
type Engine struct {
	st       *store.Store
	log      *slog.Logger
	feeBps   int
	official OfficialLookup
}

// New creates a settlement engine. feeBps is applied to notional cost on paper fills.
// official may be nil (tests only) — then Binance open/close is used as a fallback.
func New(st *store.Store, log *slog.Logger, feeBps int, official OfficialLookup) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if feeBps < 0 {
		feeBps = 0
	}
	return &Engine{st: st, log: log, feeBps: feeBps, official: official}
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
			if errors.Is(err, ErrOfficialPending) {
				e.log.Debug("settle wait official", "slug", m.Slug)
			} else {
				e.log.Warn("settle failed", "slug", m.Slug, "err", err)
			}
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
	official := false
	if e.official != nil {
		off, ready, err := e.official(ctx, m.Slug)
		if err != nil {
			return r, err
		}
		if !ready || (off != "Up" && off != "Down") {
			return r, ErrOfficialPending
		}
		outcome = off
		official = true
	} else if ok && !open.IsZero() && !closePx.IsZero() {
		// Test / no Gamma: Binance open vs close (can disagree with Chainlink TWAP).
		if closePx.GreaterThanOrEqual(open) {
			outcome = "Up"
		} else {
			outcome = "Down"
		}
	}
	r.Outcome = outcome
	r.Official = official

	winToken := ""
	switch outcome {
	case "Up":
		winToken = m.UpTokenID
	case "Down":
		winToken = m.DownTokenID
	}

	feeRate := decimal.NewFromInt(int64(e.feeBps)).Div(decimal.NewFromInt(10000))

	// Prefer per-order settlement so A/B/C can co-exist without merging PnL.
	orders, err := e.st.ListOrdersForMarket(ctx, m.Slug)
	if err != nil {
		return r, err
	}
	type leg struct {
		id, strategy, tokenID, size, price, sizeUSD, status string
		dryRun                                              bool
	}
	var legs []leg
	for _, o := range orders {
		switch o.Status {
		case "dry_run", "dry_filled", "live", "pending", "open":
			legs = append(legs, leg{
				id: o.ID, strategy: o.Strategy, tokenID: o.TokenID,
				size: o.Size, price: o.Price, sizeUSD: o.SizeUSD,
				status: o.Status, dryRun: o.DryRun,
			})
		}
	}

	// Fallback: positions only if no open orders (legacy path).
	if len(legs) == 0 {
		positions, err := e.st.ListPositions(ctx, m.Slug)
		if err != nil {
			return r, err
		}
		for _, p := range positions {
			legs = append(legs, leg{
				id: "", strategy: "?", tokenID: p.TokenID,
				size: p.Size, price: p.AvgPrice, sizeUSD: p.SizeUSD,
				status: "position", dryRun: true,
			})
		}
	}
	r.Positions = len(legs)

	totalPnL := decimal.Zero
	totalFee := decimal.Zero
	anyLive := false
	type stratPnL struct {
		pnl  decimal.Decimal
		live bool
	}
	byStrat := map[string]stratPnL{}

	for _, lg := range legs {
		if !lg.dryRun && lg.status != "position" {
			anyLive = true
		}
		size, _ := decimal.NewFromString(lg.size)
		avg, _ := decimal.NewFromString(lg.price)
		costUSD, _ := decimal.NewFromString(lg.sizeUSD)
		if costUSD.IsZero() && !size.IsZero() && !avg.IsZero() {
			costUSD = size.Mul(avg)
		}
		fee := costUSD.Mul(feeRate)
		totalFee = totalFee.Add(fee)

		var pnl decimal.Decimal
		if outcome == "Unknown" || winToken == "" {
			totalFee = totalFee.Sub(fee)
			pnl = decimal.Zero
		} else if lg.tokenID == winToken {
			pnl = size.Sub(costUSD).Sub(fee)
		} else {
			pnl = costUSD.Neg().Sub(fee)
		}
		totalPnL = totalPnL.Add(pnl)
		stKey := lg.strategy
		if stKey == "" {
			stKey = "?"
		}
		rec := byStrat[stKey]
		rec.pnl = rec.pnl.Add(pnl)
		if !lg.dryRun && lg.status != "position" {
			rec.live = true
		}
		byStrat[stKey] = rec

		if lg.id != "" {
			term := "dry_settled"
			if !lg.dryRun {
				term = "settled"
			}
			if err := e.st.UpdateOrderSettle(ctx, lg.id, term, pnl); err != nil {
				e.log.Warn("order settle update", "id", lg.id, "err", err)
			}
		}
	}

	r.PnLUSD = totalPnL
	r.FeeUSD = totalFee

	live, err := e.st.MarketHasLiveOrders(ctx, m.Slug)
	if err != nil {
		return r, err
	}
	if anyLive {
		live = true
	}
	kind := "paper_settle"
	if live {
		kind = "live_settle"
	}
	r.Kind = kind
	r.Live = live

	// One ledger line per strategy (no duplicate market total — avoids double-count in circuit breakers).
	// Compare queries should prefer orders.settle_pnl_usd; ledger kinds like paper_settle_A.
	if len(byStrat) == 0 {
		detail := fmt.Sprintf(
			`{"slug":%q,"outcome":%q,"open":%q,"close":%q,"positions":0,"fee_usd":%q,"live":%v}`,
			m.Slug, outcome, open.String(), closePx.String(), totalFee.String(), live,
		)
		if err := e.st.RecordPnL(ctx, kind, totalPnL, detail); err != nil {
			return r, err
		}
	} else {
		// Kind follows THIS strategy's orders, not "market has any live order".
		// Otherwise paper C on the same window is tagged live_settle_C and
		// trips the live-A hourly breaker.
		for st, rec := range byStrat {
			skind := "paper_settle"
			if rec.live {
				skind = "live_settle"
			}
			if st != "?" && st != "" {
				skind = skind + "_" + st
			}
			detail := fmt.Sprintf(
				`{"slug":%q,"strategy":%q,"outcome":%q,"open":%q,"close":%q,"pnl_usd":%q,"fee_bps":%d,"live":%v}`,
				m.Slug, st, outcome, open.String(), closePx.String(), rec.pnl.String(), e.feeBps, rec.live,
			)
			if err := e.st.RecordPnL(ctx, skind, rec.pnl, detail); err != nil {
				return r, err
			}
		}
	}

	// Status already set per-order; still clear any leftover statuses.
	_ = e.st.MarkOrdersStatus(ctx, m.Slug, "dry_settled", []string{"dry_run", "dry_filled"})
	_ = e.st.MarkOrdersStatus(ctx, m.Slug, "settled", []string{"pending", "live", "open", "cancelled"})

	if err := e.st.ClearPositions(ctx, m.Slug); err != nil {
		return r, err
	}
	if err := e.st.MarkMarketSettled(ctx, m.Slug, outcome, totalPnL, now); err != nil {
		return r, err
	}

	e.log.Info("market settled",
		"slug", m.Slug,
		"outcome", outcome,
		"official", official,
		"pnl_usd", totalPnL.String(),
		"fee_usd", totalFee.String(),
		"open", open.String(),
		"close", closePx.String(),
		"positions", r.Positions,
		"kind", kind,
		"live", live,
	)
	return r, nil
}
