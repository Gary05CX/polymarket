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
	cfg   config.RiskConfig
	store *store.Store

	mu           sync.Mutex
	halted       bool
	haltReason   string
	haltedAt     time.Time
}

// New creates a risk manager.
func New(cfg config.RiskConfig, st *store.Store) *Manager {
	return &Manager{cfg: cfg, store: st}
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

// Filter returns allowed signals and reasons for rejected ones.
func (m *Manager) Filter(ctx context.Context, signals []strategy.Signal) (allowed []strategy.Signal, rejected []string) {
	if halted, reason := m.Halted(); halted {
		for _, s := range signals {
			rejected = append(rejected, fmt.Sprintf("%s %s: circuit breaker (%s)", s.MarketSlug, s.Strategy, reason))
		}
		return nil, rejected
	}

	// refresh PnL-based halt
	if err := m.checkLossLimits(ctx); err != nil {
		m.SetHalt(true, err.Error())
		for _, s := range signals {
			rejected = append(rejected, fmt.Sprintf("%s %s: %v", s.MarketSlug, s.Strategy, err))
		}
		return nil, rejected
	}

	openMarkets, err := m.store.CountOpenMarkets(ctx)
	if err != nil {
		rejected = append(rejected, fmt.Sprintf("count open markets: %v", err))
		return nil, rejected
	}

	// track new markets we approve this batch
	newMarkets := map[string]bool{}

	for _, s := range signals {
		if m.cfg.HardMinSecondsLeft > 0 && s.SecondsLeft < float64(m.cfg.HardMinSecondsLeft) {
			rejected = append(rejected, fmt.Sprintf("%s: hard min seconds left", s.MarketSlug))
			continue
		}

		// size clamps
		if s.Strategy == "A" {
			// enforced again here as safety
			if s.SizeUSD.LessThan(decimal.NewFromInt(1)) || s.SizeUSD.GreaterThan(decimal.NewFromInt(3)) {
				// allow configured range but never above 3 for A as hard cap per spec
				if s.SizeUSD.GreaterThan(decimal.NewFromInt(3)) {
					s.SizeUSD = decimal.NewFromInt(3)
					if !s.Price.IsZero() {
						s.Size = s.SizeUSD.Div(s.Price)
					}
				}
			}
		}
		if s.Strategy == "B" && s.SizeUSD.GreaterThan(decimal.NewFromInt(5)) {
			s.SizeUSD = decimal.NewFromInt(5)
			if !s.Price.IsZero() {
				s.Size = s.SizeUSD.Div(s.Price)
			}
		}

		exposure, err := m.store.MarketExposureUSD(ctx, s.MarketSlug)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s: exposure: %v", s.MarketSlug, err))
			continue
		}
		if exposure.Add(s.SizeUSD).GreaterThan(m.cfg.MaxPositionUSDPerMarketDec) {
			rejected = append(rejected, fmt.Sprintf("%s: max position per market (%s+%s > %s)",
				s.MarketSlug, exposure, s.SizeUSD, m.cfg.MaxPositionUSDPerMarketDec))
			continue
		}

		// concurrent markets
		isNew := exposure.IsZero() && !newMarkets[s.MarketSlug]
		if isNew {
			if openMarkets+len(newMarkets) >= m.cfg.MaxOpenMarkets && m.cfg.MaxOpenMarkets > 0 {
				rejected = append(rejected, fmt.Sprintf("%s: max open markets %d", s.MarketSlug, m.cfg.MaxOpenMarkets))
				continue
			}
			newMarkets[s.MarketSlug] = true
		}

		if s.Price.LessThanOrEqual(decimal.Zero) || s.Size.LessThanOrEqual(decimal.Zero) {
			rejected = append(rejected, fmt.Sprintf("%s: invalid price/size", s.MarketSlug))
			continue
		}

		allowed = append(allowed, s)
	}
	return allowed, rejected
}

func (m *Manager) checkLossLimits(ctx context.Context) error {
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	hourStart := now.Truncate(time.Hour)

	daily, err := m.store.SumPnLSince(ctx, dayStart)
	if err != nil {
		return fmt.Errorf("daily pnl: %w", err)
	}
	hourly, err := m.store.SumPnLSince(ctx, hourStart)
	if err != nil {
		return fmt.Errorf("hourly pnl: %w", err)
	}

	// amount_usd negative means loss; trip if sum <= -limit
	if !m.cfg.MaxDailyLossUSDDec.IsZero() && daily.LessThanOrEqual(m.cfg.MaxDailyLossUSDDec.Neg()) {
		return fmt.Errorf("daily loss circuit breaker: pnl=%s limit=-%s", daily, m.cfg.MaxDailyLossUSDDec)
	}
	if !m.cfg.MaxHourlyLossUSDDec.IsZero() && hourly.LessThanOrEqual(m.cfg.MaxHourlyLossUSDDec.Neg()) {
		return fmt.Errorf("hourly loss circuit breaker: pnl=%s limit=-%s", hourly, m.cfg.MaxHourlyLossUSDDec)
	}
	return nil
}
