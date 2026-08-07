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
	"github.com/Gary05CX/polymarket/internal/settle"
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

	st, err := store.OpenFromOptions(store.OpenOptions{
		Driver:      cfg.Database.Driver,
		DuckDBPath:  cfg.Database.DuckDBPath,
		PostgresDSN: cfg.Database.PostgresURL,
	})
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
		"db_driver":  st.DriverName(),
		"strategy_a": cfg.StrategyA.Enabled,
		"strategy_b": cfg.StrategyB.Enabled,
		"strategy_c": cfg.StrategyC.Enabled,
	})

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
	strat := strategy.New(cfg.StrategyA, cfg.StrategyB, cfg.StrategyC, cfg.FairValue)
	rm := risk.New(cfg.Risk, st, cfg.CLOB.DryRun)
	settler := settle.New(st, log, cfg.Risk.PaperFeeBps)

	ticker := time.NewTicker(cfg.Loop.PollInterval())
	defer ticker.Stop()

	snapEvery := cfg.Loop.SnapshotEveryNTicks
	if snapEvery <= 0 {
		snapEvery = 1
	}
	rejectEvery := cfg.Loop.RejectSummaryEveryNTicks
	checkpointEvery := cfg.Loop.CheckpointEveryNTicks
	tickN := 0
	skipTally := map[string]int{}

	log.Info("entering main loop",
		"interval", cfg.Loop.PollInterval().String(),
		"snapshot_every", snapEvery,
		"checkpoint_every", checkpointEvery,
	)

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received")
			shutdownCtx, c := context.WithTimeout(context.Background(), 20*time.Second)
			defer c()
			lockCloses(shutdownCtx, feed, cfg, st, time.Now().UTC())
			spotMap := currentSpots(feed, cfg.Assets)
			if results, err := settler.RunSettlements(shutdownCtx, time.Now().UTC(), spotMap); err != nil {
				log.Warn("shutdown settle", "err", err)
			} else if len(results) > 0 {
				log.Info("shutdown settled markets", "n", len(results))
			}
			if err := exec.CancelOpen(shutdownCtx); err != nil {
				log.Warn("cancel open orders", "err", err)
			}
			if err := st.Checkpoint(shutdownCtx); err != nil {
				log.Warn("checkpoint", "err", err)
			}
			mon.Info(shutdownCtx, "bot_stop", "graceful")
			return nil
		case <-ticker.C:
			tickN++
			if err := tick(ctx, cfg, disc, feed, strat, rm, exec, settler, st, mon, tickN, snapEvery, skipTally); err != nil {
				log.Error("tick error", "err", err)
				mon.Warn(ctx, "tick_error", err.Error())
			}
			if rejectEvery > 0 && tickN%rejectEvery == 0 {
				if sum := rm.ConsumeRejectSummary(); len(sum) > 0 {
					mon.Info(ctx, "reject_summary", sum)
				}
				if len(skipTally) > 0 {
					// copy & clear
					cp := map[string]int{}
					for k, v := range skipTally {
						cp[k] = v
					}
					for k := range skipTally {
						delete(skipTally, k)
					}
					mon.Info(ctx, "strategy_skip_summary", cp)
				}
			}
			if checkpointEvery > 0 && tickN%checkpointEvery == 0 {
				if err := st.Checkpoint(ctx); err != nil {
					log.Warn("checkpoint", "err", err)
				} else {
					log.Debug("duckdb checkpoint ok")
				}
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

func currentSpots(feed *price.Feed, assets []string) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal, len(assets))
	for _, a := range assets {
		if px, _, ok := feed.Get(a); ok {
			out[a] = px
		}
	}
	return out
}

// lockCloses writes close_price for markets that just ended (best-effort at shutdown).
func lockCloses(ctx context.Context, feed *price.Feed, cfg *config.Config, st *store.Store, now time.Time) {
	seedClosePrices(ctx, st, currentSpots(feed, cfg.Assets), now)
}

func tick(
	ctx context.Context,
	cfg *config.Config,
	disc *discovery.Client,
	feed *price.Feed,
	strat *strategy.Engine,
	rm *risk.Manager,
	exec *execution.Executor,
	settler *settle.Engine,
	st *store.Store,
	mon *monitor.Monitor,
	tickN, snapEvery int,
	skipTally map[string]int,
) error {
	now := time.Now().UTC()
	spotMap := currentSpots(feed, cfg.Assets)

	// 1) Seed close prices for expired markets with open exposure, then paper-settle.
	seedClosePrices(ctx, st, spotMap, now)
	results, err := settler.RunSettlements(ctx, now, spotMap)
	if err != nil {
		mon.Logger().Warn("settle", "err", err)
	}
	for _, r := range results {
		ev := r.Kind
		if ev == "" {
			ev = "paper_settle"
		}
		mon.Info(ctx, ev, map[string]any{
			"slug":      r.Slug,
			"outcome":   r.Outcome,
			"pnl_usd":   r.PnLUSD.String(),
			"fee_usd":   r.FeeUSD.String(),
			"open":      r.Open.String(),
			"close":     r.Close.String(),
			"positions": r.Positions,
			"live":      r.Live,
		})
		// Strategy C loss streak / cooldown (manual 70% profile).
		if cfg.StrategyC.Enabled {
			if orders, err := st.ListOrdersForMarket(ctx, r.Slug); err == nil {
				for _, o := range orders {
					if o.Strategy == "C" {
						strat.RecordCOutcome(r.PnLUSD)
						if !strat.CCooldownUntil().IsZero() && time.Now().Before(strat.CCooldownUntil()) {
							mon.Info(ctx, "strategy_c_cooldown", map[string]any{
								"until":   strat.CCooldownUntil().UTC().Format(time.RFC3339),
								"pnl_usd": r.PnLUSD.String(),
							})
						}
						break
					}
				}
			}
		}
	}

	// 2) Discover active markets
	markets, err := disc.DiscoverActive(ctx, cfg.Assets, cfg.Timeframes, now)
	if err != nil {
		return fmt.Errorf("discovery: %w", err)
	}
	if len(markets) == 0 {
		return errors.New("no active markets")
	}

	writeSnap := tickN%snapEvery == 0
	openMaxAge := cfg.Loop.OpenPriceMaxAgeSec
	var allSignals []strategy.Signal

	for _, m := range markets {
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

		// Lock open_price carefully near window start.
		if spotOK {
			existing, _ := st.GetMarket(ctx, m.Slug)
			needOpen := existing == nil || existing.OpenPrice == nil || *existing.OpenPrice == ""
			if needOpen {
				age := now.Unix() - m.WindowStart
				if openMaxAge <= 0 || (age >= 0 && age <= int64(openMaxAge)) {
					op := spot.String()
					sm.OpenPrice = &op
				}
			}
		}
		if err := st.UpsertMarket(ctx, sm); err != nil {
			mon.Logger().Warn("upsert market", "slug", m.Slug, "err", err)
		}
		if spotOK && sm.OpenPrice != nil {
			_ = st.SetOpenPrice(ctx, m.Slug, *sm.OpenPrice)
		} else if spotOK {
			// late join: still set if empty so strategies can run (document as approximate)
			existing, _ := st.GetMarket(ctx, m.Slug)
			if existing == nil || existing.OpenPrice == nil || *existing.OpenPrice == "" {
				if openMaxAge <= 0 {
					_ = st.SetOpenPrice(ctx, m.Slug, spot.String())
				}
			}
		}

		// Lock close_price when window has ended (safety if still discovered near boundary).
		if spotOK && !m.EndTime.IsZero() && !now.Before(m.EndTime) {
			_ = st.SetClosePrice(ctx, m.Slug, spot.String())
		}

		open := decimal.Zero
		if row, err := st.GetMarket(ctx, m.Slug); err == nil && row != nil && row.OpenPrice != nil {
			if d, e := decimal.NewFromString(*row.OpenPrice); e == nil {
				open = d
			}
		}
		if open.IsZero() && spotOK {
			// mid-window start without open yet: use spot as provisional open for fair value only
			open = spot
		}

		midUp, midDown := m.MidUp, m.MidDown
		bidUp, askUp := m.BestBidUp, m.BestAskUp
		bidDown, askDown := m.BestBidDown, m.BestAskDown

		// Always try CLOB public book (works in dry_run with L0 client).
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

		if midDown.IsZero() && !midUp.IsZero() {
			midDown = decimal.NewFromInt(1).Sub(midUp)
		}
		if midUp.IsZero() && !midDown.IsZero() {
			midUp = decimal.NewFromInt(1).Sub(midDown)
		}

		windowSec, _ := discovery.TimeframeSeconds(m.Timeframe)
		secLeft := fairvalue.SecondsLeft(m.EndTime, now)
		if secLeft == 0 && windowSec > 0 {
			endTs := m.WindowStart + windowSec
			secLeft = float64(endTs - now.Unix())
			if secLeft < 0 {
				secLeft = 0
			}
		}
		elapsedSec := 0.0
		if m.WindowStart > 0 {
			elapsedSec = float64(now.Unix() - m.WindowStart)
			if elapsedSec < 0 {
				elapsedSec = 0
			}
		} else if windowSec > 0 && secLeft >= 0 {
			elapsedSec = float64(windowSec) - secLeft
			if elapsedSec < 0 {
				elapsedSec = 0
			}
		}

		sigma := feed.RealizedSigma(m.Asset)
		fv := fairvalue.Compute(cfg.FairValue, open, spot, midUp, midDown, sigma, windowSec, secLeft)
		posUSD, _ := st.PositionUSD(ctx, m.Slug)

		if writeSnap {
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
		}

		if !spotOK {
			mon.Logger().Debug("skip market: stale spot", "slug", m.Slug)
			continue
		}
		if open.IsZero() {
			mon.Logger().Debug("skip market: no open_price yet", "slug", m.Slug)
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
			ElapsedSec:  elapsedSec,
			PositionUSD: posUSD,
		}
		sigs, skips := strat.Evaluate(in)
		allSignals = append(allSignals, sigs...)
		for _, sk := range skips {
			key := sk.Strategy + ":" + sk.Reason
			// collapse numeric detail in reason for tally keys
			if skipTally != nil {
				skipTally[key]++
			}
			mon.Logger().Debug("strategy_skip", "strategy", sk.Strategy, "slug", sk.MarketSlug, "reason", sk.Reason)
		}

		mon.Logger().Debug("market tick",
			"slug", m.Slug,
			"spot", spot.String(),
			"open", open.String(),
			"mid_up", midUp.String(),
			"fair_up", fv.PUp.String(),
			"sec_left", secLeft,
			"signals", len(sigs),
			"skips", len(skips),
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

// seedClosePrices sets close_price for unsettled markets whose end_time has passed.
func seedClosePrices(ctx context.Context, st *store.Store, spotMap map[string]decimal.Decimal, now time.Time) {
	due, err := st.ListMarketsDueForSettle(ctx, now)
	if err != nil {
		return
	}
	for _, m := range due {
		if px, ok := spotMap[m.Asset]; ok && !px.IsZero() {
			_ = st.SetClosePrice(ctx, m.Slug, px.String())
		}
	}
}
