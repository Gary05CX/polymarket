package resolve

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/market"
	"github.com/Gary05CX/polymarket/internal/store"
)

func WaitOfficial(ctx context.Context, cfg config.Resolver, md *market.Client, st *store.Store, runID, slug string) {
	if !cfg.Enabled {
		return
	}
	wait := time.Duration(cfg.PollAfterCloseSec * float64(time.Second))
	interval := time.Duration(cfg.PollIntervalSec * float64(time.Second))
	timeout := time.Duration(cfg.PollTimeoutSec * float64(time.Second))
	if interval <= 0 {
		interval = 15 * time.Second
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(wait):
	}
	deadline := time.Now().Add(timeout)
	for {
		closed, winner, raw, err := md.Official(slug)
		if err != nil {
			slog.Warn("resolver gamma", "slug", slug, "err", err)
		} else if closed && winner != "" {
			_ = st.UpsertResolution(slug, "official_gamma", winner, 0, 0, raw)
			fillPnL(st, runID, slug, winner)
			slog.Info("official resolved", "slug", slug, "winner", winner)
			return
		}
		if time.Now().After(deadline) {
			_ = st.UpsertResolution(slug, "official_gamma", "pending", 0, 0, raw)
			slog.Warn("resolver timeout", "slug", slug)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func fillPnL(st *store.Store, runID, slug, winner string) {
	opens, err := st.OpenFills(runID)
	if err != nil {
		slog.Warn("open fills", "err", err)
		return
	}
	for _, o := range opens {
		if o.Slug != slug {
			continue
		}
		win := strings.EqualFold(o.Side, winner)
		exit := 0.0
		if win {
			exit = 1
		}
		if err := st.InsertPnL(o.RunID, o.Mode, o.StrategyID, o.Slug, o.ID, o.FillID, o.FillModel, o.Shares, o.Entry, exit, o.Fee, win); err != nil {
			slog.Warn("pnl insert", "err", err)
		}
	}
}
