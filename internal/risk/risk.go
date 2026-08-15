// Package risk enforces position, concurrency, and loss circuit breakers.
package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/Gary05CX/polymarket/internal/strategy"
	"github.com/shopspring/decimal"
)

// Manager checks signals against risk limits.
type Manager struct {
	cfg          config.RiskConfig
	store        *store.Store
	dryRun       bool
	effectiveDry func(strategy string) bool

	mu         sync.Mutex
	halted     bool
	haltReason string
	haltedAt   time.Time

	// reject tallies for periodic summary
	rejectMu sync.Mutex
	rejects  map[string]int
}

// New creates a risk manager. effectiveDry may be nil (then dryRun applies to all).
func New(cfg config.RiskConfig, st *store.Store, dryRun bool, effectiveDry func(string) bool) *Manager {
	return &Manager{
		cfg:          cfg,
		store:        st,
		dryRun:       dryRun,
		effectiveDry: effectiveDry,
		rejects:      make(map[string]int),
	}
}

func (m *Manager) isPaper(strategy string) bool {
	if m.effectiveDry != nil {
		return m.effectiveDry(strategy)
	}
	return m.dryRun
}

// Halted reports if new orders are blocked.
func (m *Manager) Halted() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.halted, m.haltReason
}

// SetHalt manually trips/clears the breaker.
func (m *Manager) SetHalt(halt bool, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.halted = halt
	m.haltReason = reason
	if halt {
		m.haltedAt = time.Now()
	}
}

// ConsumeRejectSummary returns and clears reject reason counts.
func (m *Manager) ConsumeRejectSummary() map[string]int {
	m.rejectMu.Lock()
	defer m.rejectMu.Unlock()
	out := m.rejects
	m.rejects = make(map[string]int)
	return out
}

func (m *Manager) tally(reason string) {
	m.rejectMu.Lock()
	m.rejects[reason]++
	m.rejectMu.Unlock()
}

// Filter returns allowed signals and reasons for rejected ones.
func (m *Manager) Filter(ctx context.Context, signals []strategy.Signal) (allowed []strategy.Signal, rejected []string) {
	if halted, reason := m.Halted(); halted {
		for _, s := range signals {
			msg := fmt.Sprintf("%s %s: circuit breaker (%s)", s.MarketSlug, s.Strategy, reason)
			rejected = append(rejected, msg)
			m.tally("circuit_breaker")
		}
		return nil, rejected
	}

	if err := m.checkLossLimits(ctx); err != nil {
		m.SetHalt(true, err.Error())
		for _, s := range signals {
			msg := fmt.Sprintf("%s %s: %v", s.MarketSlug, s.Strategy, err)
			rejected = append(rejected, msg)
			m.tally("loss_limit")
		}
		return nil, rejected
	}

	now := time.Now().UTC()
	openMarkets, err := m.store.CountOpenMarkets(ctx, now)
	if err != nil {
		rejected = append(rejected, fmt.Sprintf("count open markets: %v", err))
		m.tally("count_error")
		return nil, rejected
	}

	batchSeenStrat := map[string]bool{} // market|strategy
	batchSeenMarket := map[string]bool{}
	newMarkets := map[string]bool{}

	for _, s := range signals {
		key := s.MarketSlug + "|" + s.Strategy

		if m.cfg.HardMinSecondsLeft > 0 && s.SecondsLeft < float64(m.cfg.HardMinSecondsLeft) {
			rejected = append(rejected, fmt.Sprintf("%s: hard min seconds left", s.MarketSlug))
			m.tally("hard_min_seconds")
			continue
		}

		// A and B mutually exclusive on the same market window.
		if m.cfg.OneOrderPerMarket {
			if batchSeenMarket[s.MarketSlug] {
				rejected = append(rejected, fmt.Sprintf("%s %s: market already has order in batch", s.MarketSlug, s.Strategy))
				m.tally("market_batch_dup")
				continue
			}
			exists, err := m.store.HasMarketOrder(ctx, s.MarketSlug)
			if err != nil {
				rejected = append(rejected, fmt.Sprintf("%s: has market order check: %v", s.MarketSlug, err))
				m.tally("has_order_err")
				continue
			}
			if exists {
				rejected = append(rejected, fmt.Sprintf("%s %s: market already ordered this window", s.MarketSlug, s.Strategy))
				continue
			}
		}

		if m.cfg.OneOrderPerStrategy {
			if batchSeenStrat[key] {
				rejected = append(rejected, fmt.Sprintf("%s %s: duplicate in batch", s.MarketSlug, s.Strategy))
				m.tally("batch_dup")
				continue
			}
			exists, err := m.store.HasStrategyOrder(ctx, s.MarketSlug, s.Strategy)
			if err != nil {
				rejected = append(rejected, fmt.Sprintf("%s %s: has order check: %v", s.MarketSlug, s.Strategy, err))
				m.tally("has_order_err")
				continue
			}
			if exists {
				// Expected after first fill — don't spam reject_summary.
				rejected = append(rejected, fmt.Sprintf("%s %s: already ordered this window", s.MarketSlug, s.Strategy))
				continue
			}
		}

		// Hard ceiling for a single order: risk.max_position_usd_per_market.
		// Strategy sizes come from yaml (size_min/max, max_size_usd) — do not
		// re-hardcode $3/$5 here or size-scaled profiles silently get clipped.
		if !m.cfg.MaxPositionUSDPerMarketDec.IsZero() && s.SizeUSD.GreaterThan(m.cfg.MaxPositionUSDPerMarketDec) {
			s.SizeUSD = m.cfg.MaxPositionUSDPerMarketDec
			if !s.Price.IsZero() {
				s.Size = s.SizeUSD.Div(s.Price)
			}
		}

		if !m.cfg.MaxSpreadDec.IsZero() && !s.BestBid.IsZero() && !s.BestAsk.IsZero() {
			spread := s.BestAsk.Sub(s.BestBid)
			if spread.GreaterThan(m.cfg.MaxSpreadDec) {
				rejected = append(rejected, fmt.Sprintf("%s: spread %s > max %s", s.MarketSlug, spread, m.cfg.MaxSpreadDec))
				m.tally("max_spread")
				continue
			}
		}

		// Paper fill at mid for global dry-run or paper_only strategies (B/C).
		if m.isPaper(s.Strategy) && m.cfg.PaperFillAtMid && !s.MarketMid.IsZero() {
			s.Price = s.MarketMid
			if !s.Price.IsZero() {
				s.Size = s.SizeUSD.Div(s.Price)
			}
		}

		exposure, err := m.store.MarketExposureUSD(ctx, s.MarketSlug)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s: exposure: %v", s.MarketSlug, err))
			m.tally("exposure_err")
			continue
		}
		if exposure.Add(s.SizeUSD).GreaterThan(m.cfg.MaxPositionUSDPerMarketDec) {
			rejected = append(rejected, fmt.Sprintf("%s: max position per market (%s+%s > %s)",
				s.MarketSlug, exposure, s.SizeUSD, m.cfg.MaxPositionUSDPerMarketDec))
			m.tally("max_position")
			continue
		}

		isNew := exposure.IsZero() && !newMarkets[s.MarketSlug]
		if isNew {
			if m.cfg.MaxOpenMarkets > 0 && openMarkets+len(newMarkets) >= m.cfg.MaxOpenMarkets {
				rejected = append(rejected, fmt.Sprintf("%s: max open markets %d", s.MarketSlug, m.cfg.MaxOpenMarkets))
				m.tally("max_open_markets")
				continue
			}
			newMarkets[s.MarketSlug] = true
		}

		if s.Price.LessThanOrEqual(decimal.Zero) || s.Size.LessThanOrEqual(decimal.Zero) {
			rejected = append(rejected, fmt.Sprintf("%s: invalid price/size", s.MarketSlug))
			m.tally("invalid_px")
			continue
		}

		batchSeenStrat[key] = true
		batchSeenMarket[s.MarketSlug] = true
		allowed = append(allowed, s)
	}
	return allowed, rejected
}

func (m *Manager) checkLossLimits(ctx context.Context) error {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	hourStart := now.Truncate(time.Hour)

	// Live A must not be halted by paper B/C settle losses.
	liveOnly := !m.dryRun
	daily, err := m.store.SumPnLSinceLiveOnly(ctx, dayStart, liveOnly)
	if err != nil {
		return fmt.Errorf("daily pnl: %w", err)
	}
	hourly, err := m.store.SumPnLSinceLiveOnly(ctx, hourStart, liveOnly)
	if err != nil {
		return fmt.Errorf("hourly pnl: %w", err)
	}

	if !m.cfg.MaxDailyLossUSDDec.IsZero() && daily.LessThanOrEqual(m.cfg.MaxDailyLossUSDDec.Neg()) {
		return fmt.Errorf("daily loss circuit breaker: pnl=%s limit=-%s", daily, m.cfg.MaxDailyLossUSDDec)
	}
	if !m.cfg.MaxHourlyLossUSDDec.IsZero() && hourly.LessThanOrEqual(m.cfg.MaxHourlyLossUSDDec.Neg()) {
		return fmt.Errorf("hourly loss circuit breaker: pnl=%s limit=-%s", hourly, m.cfg.MaxHourlyLossUSDDec)
	}
	return nil
}
