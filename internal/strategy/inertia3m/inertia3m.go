package inertia3m

import (
	"context"
	"fmt"
	"math"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/domain"
)

type Strat struct{ Cfg config.Inertia3m }

func (s Strat) ID() string     { return "inertia_3m" }
func (s Strat) Source() string { return "ratrimaa/btc-5m-polymarket-bot" }

func (s Strat) Evaluate(_ context.Context, snap domain.MarketSnapshot) (*domain.Signal, *domain.Reject, error) {
	if snap.Elapsed < s.Cfg.EntryDelaySec {
		return nil, nil, nil
	}
	if snap.SecondsLeft < s.Cfg.MinTimeRemaining {
		return nil, &domain.Reject{ReasonCode: "too_late", Reason: "remaining < min_time", Slug: snap.Market.Slug}, nil
	}
	ta := Analyze(snap.Klines1m, snap.WindowOpenPx, snap.BinancePrice, s.Cfg)
	feat := ta.Features
	rej := func(code, reason string) (*domain.Signal, *domain.Reject, error) {
		return nil, &domain.Reject{ReasonCode: code, Reason: reason, Features: feat, Slug: snap.Market.Slug}, nil
	}
	if ta.Side == "" {
		return rej("flat", ta.Reason)
	}
	pmPx, token := sideQuote(snap, ta.Side)
	if pmPx <= 0 {
		return rej("no_pm_price", "no polymarket price")
	}
	edge := ta.Confidence - pmPx
	feat["edge"] = edge
	feat["market_prob"] = pmPx
	if edge < s.Cfg.MinEdge {
		return rej("edge", fmt.Sprintf("edge %.3f < %.2f", edge, s.Cfg.MinEdge))
	}
	sig := &domain.Signal{
		StrategyID:     s.ID(),
		StrategySource: s.Source(),
		Asset:          snap.Market.Asset,
		MarketSlug:     snap.Market.Slug,
		ConditionID:    snap.Market.ConditionID,
		Side:           ta.Side,
		TokenID:        token,
		Confidence:     ta.Confidence,
		Score:          ta.Score,
		Features:       feat,
		Reason:         ta.Reason,
		PMPrice:        pmPx,
		BinancePrice:   snap.BinancePrice,
		WindowOpenPx:   snap.WindowOpenPx,
		SecondsLeft:    snap.SecondsLeft,
	}
	return sig, nil, nil
}

type TA struct {
	Side       string
	Score      float64
	Confidence float64
	Reason     string
	Features   map[string]any
}

func Analyze(kl []domain.Candle, open, px float64, cfg config.Inertia3m) TA {
	feat := map[string]any{"px": px, "open": open}
	if px <= 0 || open <= 0 {
		return TA{Reason: "no price", Features: feat}
	}
	dist := (px - open) / open * 100
	abs := math.Abs(dist)
	feat["move_pct"] = dist
	var up, down int
	switch {
	case abs > cfg.StrongMovePct:
		if dist > 0 {
			up += 5
		} else {
			down += 5
		}
	case abs > cfg.FlatThresholdPct:
		if dist > 0 {
			up += 3
		} else {
			down += 3
		}
	default:
		return TA{Reason: fmt.Sprintf("flat %.4f%%", dist), Features: feat}
	}
	if n := len(kl); n >= 1 {
		last := kl[n-1]
		green := last.Close > last.Open
		if green {
			up++
		} else {
			down++
		}
		feat["last_green"] = green
	}
	if n := len(kl); n >= 3 {
		vols := make([]float64, n)
		for i, c := range kl {
			vols[i] = c.Volume
		}
		avgFirst := (vols[0] + vols[1]) / 2
		if avgFirst > 0 {
			ratio := vols[n-1] / avgFirst
			feat["vol_ratio"] = ratio
			if ratio > 1.5 {
				if dist > 0 {
					up++
				} else {
					down++
				}
			}
		}
	}
	score := up
	side := "Up"
	if down > up {
		score, side = down, "Down"
	}
	conf := float64(score) / 7
	ok := score >= cfg.MinScoreStrong || score >= cfg.MinScoreModerate
	if !ok {
		return TA{Reason: fmt.Sprintf("score %d too low", score), Features: feat}
	}
	feat["score_up"] = up
	feat["score_down"] = down
	return TA{Side: side, Score: float64(score), Confidence: conf, Reason: fmt.Sprintf("%s score=%d conf=%.2f", side, score, conf), Features: feat}
}

func sideQuote(snap domain.MarketSnapshot, side string) (float64, string) {
	if side == "Up" {
		return snap.PMMidUp, snap.Market.UpTokenID
	}
	return snap.PMMidDown, snap.Market.DownTokenID
}
