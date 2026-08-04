// Command bot is the Polymarket 5m/15m crypto auto-trading service.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/discovery"
	"github.com/Gary05CX/polymarket/internal/execution"
	"github.com/Gary05CX/polymarket/internal/fairvalue"
	"github.com/Gary05CX/polymarket/internal/monitor"
	"github.com/Gary05CX/polymarket/internal/price"
	"github.com/Gary05CX/polymarket/internal/risk"
	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/Gary05CX/polymarket/internal/strategy"
	"github.com/shopspring/decimal"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	st, err := store.Open(cfg.DuckDB.Path, store.SchemaDuckDB)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	mon := monitor.New(cfg.Monitor.LogLevel, st, cfg.WebhookURL)
	log := mon.Logger()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mon.Info(ctx, "bot_start", map[string]any{
		"assets":     cfg.Assets,
		"timeframes": cfg.Timeframes,
		"dry_run":    cfg.CLOB.DryRun,
	})

	// Price feed — only subscribe configured assets
	filtered := map[string]string{}
	for _, a := range cfg.Assets {
		if sym, ok := cfg.Binance.Symbols[a]; ok {
			filtered[a] = sym
		}
	}
	feed := price.NewFeed(cfg.Binance.WSBase, filtered, cfg.Binance.StaleAfter(), cfg.FairValue.VolWindow, log)
	if err := feed.Start(ctx); err != nil {
		return fmt.Errorf("price feed: %w", err)
	}
	defer feed.Stop()

	// wait briefly for first ticks
	waitPrices(ctx, feed, cfg.Assets, 8*time.Second, log)

	disc := discovery.New(
		cfg.Gamma.BaseURL,
		cfg.Gamma.Timeout(),
		cfg.Discovery.CacheTTL(),
		time.Duration(cfg.Discovery.PreviousWindowGraceSec)*time.Second,
	)

	clob, err := execution.NewLiveCLOB(cfg, log)
	if err != nil {
		return fmt.Errorf("clob: %w", err)
	}
	exec := execution.New(cfg, clob, st, log)
	strat := strategy.New(cfg.StrategyA, cfg.StrategyB)
	rm := risk.New(cfg.Risk, st)

	ticker := time.NewTicker(cfg.Loop.PollInterval())
	defer ticker.Stop()

	log.Info("entering main loop", "interval", cfg.Loop.PollInterval().String())

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received")
			shutdownCtx, c := context.WithTimeout(context.Background(), 15*time.Second)
			defer c()
			if err := exec.CancelOpen(shutdownCtx); err != nil {
				log.Warn("cancel open orders", "err", err)
			}
			mon.Info(shutdownCtx, "bot_stop", "graceful")
			return nil
		case <-ticker.C:
			if err := tick(ctx, cfg, disc, feed, strat, rm, exec, st, mon); err != nil {
				log.Error("tick error", "err", err)
				mon.Warn(ctx, "tick_error", err.Error())
			}
		}
	}
}

func waitPrices(ctx context.Context, feed *price.Feed, assets []string, timeout time.Duration, log interface {
	Info(string, ...any)
	Warn(string, ...any)
}) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		okAll := true
		for _, a := range assets {
			if _, _, ok := feed.Get(a); !ok {
				okAll = false
				break
			}
		}
		if okAll {
			log.Info("binance prices ready")
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	log.Warn("binance prices not fully ready; continuing")
}

func tick(
	ctx context.Context,
	cfg *config.Config,
	disc *discovery.Client,
	feed *price.Feed,
	strat *strategy.Engine,
	rm *risk.Manager,
	exec *execution.Executor,
	st *store.Store,
	mon *monitor.Monitor,
) error {
	now := time.Now().UTC()
	markets, err := disc.DiscoverActive(ctx, cfg.Assets, cfg.Timeframes, now)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	if len(markets) == 0 {
		return errors.New("no active markets")
	}

	var allSignals []strategy.Signal

	for _, m := range markets {
		// persist market
		sm := store.Market{
			Slug:        m.Slug,
			ConditionID: m.ConditionID,
			UpTokenID:   m.UpTokenID,
			DownTokenID: m.DownTokenID,
			Asset:       m.Asset,
			Timeframe:   m.Timeframe,
			WindowStart: m.WindowStart,
			Title:       m.Title,
		}
		if !m.EndTime.IsZero() {
			t := m.EndTime
			sm.EndTime = &t
		}
		if !m.EventStart.IsZero() {
			t := m.EventStart
			sm.EventStart = &t
		}

		spot, _, spotOK := feed.Get(m.Asset)
		if spotOK {
			// set open price once
			existing, _ := st.GetMarket(ctx, m.Slug)
			if existing == nil || existing.OpenPrice == nil || *existing.OpenPrice == "" {
				op := spot.String()
				sm.OpenPrice = &op
			}
		}
		if err := st.UpsertMarket(ctx, sm); err != nil {
			mon.Logger().Warn("upsert market", "slug", m.Slug, "err", err)
		}
		if spotOK {
			_ = st.SetOpenPrice(ctx, m.Slug, spot.String())
		}

		// reload open price from DB
		open := decimal.Zero
		if row, err := st.GetMarket(ctx, m.Slug); err == nil && row != nil && row.OpenPrice != nil {
			if d, e := decimal.NewFromString(*row.OpenPrice); e == nil {
				open = d
			}
		}
		if open.IsZero() && spotOK {
			open = spot
		}

		// mids: prefer CLOB book when available; fall back to Gamma
		midUp, midDown := m.MidUp, m.MidDown
		bidUp, askUp := m.BestBidUp, m.BestAskUp
		bidDown, askDown := m.BestBidDown, m.BestAskDown

		if !cfg.CLOB.DryRun {
			if upBook, downBook, err := exec.RefreshMids(ctx, m.UpTokenID, m.DownTokenID); err == nil {
				if !upBook.Mid.IsZero() {
					midUp = upBook.Mid
					bidUp, askUp = upBook.Bid, upBook.Ask
				}
				if !downBook.Mid.IsZero() {
					midDown = downBook.Mid
					bidDown, askDown = downBook.Bid, downBook.Ask
				}
			}
		}

		// if one mid missing, use complement
		if midDown.IsZero() && !midUp.IsZero() {
			midDown = decimal.NewFromInt(1).Sub(midUp)
		}
		if midUp.IsZero() && !midDown.IsZero() {
			midUp = decimal.NewFromInt(1).Sub(midDown)
		}

		windowSec, _ := discovery.TimeframeSeconds(m.Timeframe)
		secLeft := fairvalue.SecondsLeft(m.EndTime, now)
		if secLeft == 0 && windowSec > 0 {
			// estimate from window start if end missing
			endTs := m.WindowStart + windowSec
			secLeft = float64(endTs - now.Unix())
			if secLeft < 0 {
				secLeft = 0
			}
		}

		sigma := feed.RealizedSigma(m.Asset)
		fv := fairvalue.Compute(cfg.FairValue, open, spot, midUp, midDown, sigma, windowSec, secLeft)

		posUSD, _ := st.PositionUSD(ctx, m.Slug)

		_ = st.InsertSnapshot(ctx, store.Snapshot{
			Asset:      m.Asset,
			Spot:       spot.String(),
			MarketSlug: m.Slug,
			MidUp:      midUp.String(),
			MidDown:    midDown.String(),
			FairUp:     fv.PUp.String(),
			FairDown:   fv.PDown.String(),
			OpenPrice:  open.String(),
		})

		if !spotOK {
			mon.Logger().Debug("skip market: stale spot", "slug", m.Slug)
			continue
		}

		in := strategy.MarketInput{
			Slug:        m.Slug,
			Asset:       m.Asset,
			Timeframe:   m.Timeframe,
			UpTokenID:   m.UpTokenID,
			DownTokenID: m.DownTokenID,
			MidUp:       midUp,
			MidDown:     midDown,
			BestBidUp:   bidUp,
			BestAskUp:   askUp,
			BestBidDown: bidDown,
			BestAskDown: askDown,
			OpenPrice:   open,
			Spot:        spot,
			FairUp:      fv.PUp,
			FairDown:    fv.PDown,
			EdgeUp:      fv.EdgeUp,
			EdgeDown:    fv.EdgeDown,
			SecondsLeft: secLeft,
			PositionUSD: posUSD,
		}
		sigs := strat.Evaluate(in)
		allSignals = append(allSignals, sigs...)

		mon.Logger().Debug("market tick",
			"slug", m.Slug,
			"spot", spot.String(),
			"open", open.String(),
			"mid_up", midUp.String(),
			"mid_down", midDown.String(),
			"fair_up", fv.PUp.String(),
			"sec_left", secLeft,
			"signals", len(sigs),
		)
	}

	allowed, rejected := rm.Filter(ctx, allSignals)
	for _, r := range rejected {
		mon.Logger().Debug("signal rejected", "reason", r)
	}
	if halted, reason := rm.Halted(); halted {
		mon.Critical(ctx, "circuit_breaker", reason)
	}

	for _, sig := range allowed {
		if _, err := exec.PlaceSignal(ctx, sig); err != nil {
			mon.Error(ctx, "order_error", map[string]any{
				"slug":     sig.MarketSlug,
				"strategy": sig.Strategy,
				"err":      err.Error(),
			})
		} else {
			mon.Info(ctx, "order_signal", map[string]any{
				"strategy": sig.Strategy,
				"slug":     sig.MarketSlug,
				"outcome":  sig.Outcome,
				"price":    sig.Price.String(),
				"size_usd": sig.SizeUSD.String(),
				"edge":     sig.Edge.String(),
				"dry_run":  cfg.CLOB.DryRun,
			})
		}
	}
	return nil
}
