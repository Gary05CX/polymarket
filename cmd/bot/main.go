package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/exec"
	"github.com/Gary05CX/polymarket/internal/market"
	"github.com/Gary05CX/polymarket/internal/resolve"
	"github.com/Gary05CX/polymarket/internal/store"
	"github.com/Gary05CX/polymarket/internal/strategy"
	"github.com/Gary05CX/polymarket/internal/strategy/inertia3m"
	"github.com/Gary05CX/polymarket/internal/strategy/windowdelta"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfgPath, mode, stratName, maxW := config.ParseFlags()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	cfg.ApplyOverrides(mode, stratName, maxW)
	if err := cfg.LiveReady(); err != nil {
		slog.Error("refusing start", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.Database.Driver, cfg.Database.DSN, cfg.SQLDir)
	if err != nil {
		slog.Error("store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	strat := pick(cfg)
	runID := store.NewID()
	if err := st.InsertRun(runID, cfg.Mode, strat.ID(), strat.Source(), cfg, cfg.MaxWindows); err != nil {
		slog.Error("insert run", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.EndRun(runID) }()

	md := market.New(cfg.Polymarket.GammaURL, cfg.Polymarket.ClobURL, cfg.Binance.RestURL, cfg.Clock.Sync)
	if err := md.SyncClock(); err != nil {
		slog.Warn("clock sync failed, using local", "err", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("start", "mode", cfg.Mode, "strategy", strat.ID(), "driver", cfg.Database.Driver, "max_windows", cfg.MaxWindows, "run_id", runID)

	if err := loop(ctx, cfg, md, st, strat, runID); err != nil && err != context.Canceled {
		slog.Error("loop", "err", err)
		os.Exit(1)
	}
	n, wr, pnl, _ := st.Summary(context.Background(), runID)
	slog.Info("summary", "fills_resolved", n, "win_rate", wr, "pnl", pnl)
}

func pick(cfg *config.Config) strategy.Strategy {
	switch cfg.Strategy {
	case "inertia_3m":
		return inertia3m.Strat{Cfg: cfg.Strategies.Inertia3m}
	default:
		return windowdelta.Strat{Cfg: cfg.Strategies.WindowDelta}
	}
}

func loop(ctx context.Context, cfg *config.Config, md *market.Client, st *store.Store, strat strategy.Strategy, runID string) error {
	windows := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := market.WindowStart(md.Now())
		end := start.Add(5 * time.Minute)
		slugs := runWindow(ctx, cfg, md, st, strat, runID, start, end)
		windows++
		if cfg.Resolver.Enabled {
			for _, slug := range slugs {
				resolve.WaitOfficial(ctx, cfg.Resolver, md, st, runID, slug)
			}
		}
		if cfg.MaxWindows > 0 && windows >= cfg.MaxWindows {
			return nil
		}
		next := market.WindowStart(md.Now().Add(5 * time.Second))
		if !next.After(start) {
			next = start.Add(5 * time.Minute)
		}
		sleepUntil(ctx, md, next)
	}
}

func runWindow(ctx context.Context, cfg *config.Config, md *market.Client, st *store.Store, strat strategy.Strategy, runID string, start, end time.Time) []string {
	seen := map[string]bool{}
	var slugs []string
	note := func(slug string) {
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}

	if cfg.Strategy == "inertia_3m" {
		wake := start.Add(time.Duration(cfg.Strategies.Inertia3m.EntryDelaySec * float64(time.Second)))
		sleepUntil(ctx, md, wake)
	} else {
		wake := end.Add(-time.Duration(cfg.Strategies.WindowDelta.WakeBeforeSec) * time.Second)
		sleepUntil(ctx, md, wake)
	}

	poll := 3 * time.Second
	if cfg.Strategy == "inertia_3m" {
		poll = time.Duration(cfg.Strategies.Inertia3m.PollIntervalSec * float64(time.Second))
	} else {
		poll = time.Duration(cfg.Strategies.WindowDelta.PollIntervalSec * float64(time.Second))
	}
	if poll <= 0 {
		poll = 3 * time.Second
	}

	for md.Now().Before(end) {
		if ctx.Err() != nil {
			return slugs
		}
		for _, asset := range cfg.Assets {
			if cfg.Strategy == "inertia_3m" && !strings.EqualFold(asset, "BTC") {
				continue
			}
			sym := cfg.Binance.Symbols[strings.ToUpper(asset)]
			if sym == "" {
				sym = strings.ToUpper(asset) + "USDT"
			}
			snap, err := md.Snapshot(asset, sym, start)
			if err != nil {
				slog.Warn("snapshot", "asset", asset, "err", err)
				continue
			}
			note(snap.Market.Slug)
			_ = st.UpsertMarket(snap.Market)
			if snap.BinancePrice > 0 {
				_ = st.InsertTick(runID, snap.Market.Slug, "binance", snap.Now, snap.BinancePrice)
			}
			sig, rej, err := strat.Evaluate(ctx, snap)
			if err != nil {
				slog.Warn("evaluate", "err", err)
				continue
			}
			if rej != nil {
				_ = st.InsertRejected(runID, cfg.Mode, strat.ID(), strat.Source(), *rej)
				slog.Info("skip", "slug", snap.Market.Slug, "reason", rej.ReasonCode)
				continue
			}
			if sig == nil {
				continue
			}
			if st.HasOrder(runID, sig.MarketSlug) {
				continue
			}
			_ = st.InsertSignal(runID, cfg.Mode, sig)
			o, f, r, err := exec.BuyMinSize(cfg.Execution, cfg.Mode, snap, sig, runID)
			if err != nil {
				slog.Warn("buy", "err", err)
				continue
			}
			if r != nil {
				_ = st.InsertRejected(runID, cfg.Mode, strat.ID(), strat.Source(), *r)
				slog.Info("skip", "slug", snap.Market.Slug, "reason", r.ReasonCode)
				continue
			}
			if err := st.InsertOrder(o); err != nil {
				slog.Warn("order insert (maybe duplicate)", "err", err)
				continue
			}
			_ = st.InsertFill(f)
			_ = st.UpsertPosition(runID, o.MarketSlug, o.Side, f.Shares, f.Price)
			slog.Info("dry fill", "slug", o.MarketSlug, "side", o.Side, "shares", f.Shares, "px", f.Price, "fee", f.FeeUSDC)
		}
		sleepFor(ctx, poll)
	}
	return slugs
}

func sleepUntil(ctx context.Context, md *market.Client, t time.Time) {
	d := t.Sub(md.Now())
	if d > 0 {
		sleepFor(ctx, d)
	}
}

func sleepFor(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
