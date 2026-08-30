package windowdelta

import (
	"context"
	"fmt"
	"math"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/domain"
)

type Strat struct{ Cfg config.WindowDelta }

func (s Strat) ID() string     { return "window_delta" }
func (s Strat) Source() string { return "jmazzini/5m-poly-bot" }

func (s Strat) Evaluate(_ context.Context, snap domain.MarketSnapshot) (*domain.Signal, *domain.Reject, error) {
	if snap.SecondsLeft < s.Cfg.EntrySecondsMin || snap.SecondsLeft > s.Cfg.EntrySecondsMax {
		return nil, nil, nil
	}
	ta := Analyze(snap.BinancePrice, snap.WindowOpenPx, snap.Klines1m, snap.Klines5m, s.Cfg)
	feat := ta.Features
	rej := func(code, reason string) (*domain.Signal, *domain.Reject, error) {
		return nil, &domain.Reject{ReasonCode: code, Reason: reason, Features: feat, Slug: snap.Market.Slug}, nil
	}
	if ta.SkipCode != "" {
		return rej(ta.SkipCode, ta.Reason)
	}
	pmSide, pmPx, token := leading(snap)
	if pmPx <= 0 {
		return rej("no_pm_price", "no polymarket mid")
	}
	pmin := s.Cfg.PriceMin[snap.Market.Asset]
	if pmin == 0 {
		pmin = 0.94
	}
	if pmPx < pmin {
		return rej("price_low", fmt.Sprintf("PM %.3f < %.2f", pmPx, pmin))
	}
	if pmPx > s.Cfg.PriceMax {
		return rej("price_high", fmt.Sprintf("PM %.3f > %.2f", pmPx, s.Cfg.PriceMax))
	}
	if ta.Confidence < s.Cfg.MinConfidence {
		return rej("confidence", fmt.Sprintf("conf %.2f < %.2f", ta.Confidence, s.Cfg.MinConfidence))
	}
	if ta.Direction != pmSide {
		return rej("direction_mismatch", fmt.Sprintf("binance %s vs PM %s", ta.Direction, pmSide))
	}
	sig := &domain.Signal{
		StrategyID:     s.ID(),
		StrategySource: s.Source(),
		Asset:          snap.Market.Asset,
		MarketSlug:     snap.Market.Slug,
		ConditionID:    snap.Market.ConditionID,
		Side:           ta.Direction,
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
	Score      float64
	Confidence float64
	Direction  string
	Reason     string
	SkipCode   string
	Features   map[string]any
}

func Analyze(px, open float64, k1m, k5m []domain.Candle, cfg config.WindowDelta) TA {
	feat := map[string]any{"px": px, "open": open}
	if px <= 0 || open <= 0 {
		return TA{SkipCode: "no_binance", Reason: "no binance price", Features: feat}
	}
	delta := (px - open) / open
	deltaPct := math.Abs(delta) * 100
	feat["delta_pct"] = deltaPct
	if atr, rng, ok := atrSkip(k5m, cfg); ok {
		feat["atr"] = atr
		feat["range"] = rng
		return TA{SkipCode: "atr", Reason: fmt.Sprintf("range %.2f > %.1fx ATR %.2f", rng, cfg.ATRMultiplier, atr), Features: feat}
	}
	if math.Abs(delta) < cfg.DeltaSkip {
		return TA{SkipCode: "delta_small", Reason: fmt.Sprintf("delta %.4f%% < skip", deltaPct), Features: feat}
	}
	w := deltaWeight(math.Abs(delta), cfg)
	score := float64(w)
	if delta < 0 {
		score = -score
	}
	if len(k1m) >= 2 {
		prev, last := k1m[len(k1m)-2].Close, k1m[len(k1m)-1].Close
		up := last > prev
		if (delta > 0 && up) || (delta < 0 && !up) {
			if score > 0 {
				score += 2
			} else {
				score -= 2
			}
		}
		feat["momentum_up"] = up
	}
	conf := math.Min(math.Abs(score)/9, 1)
	dir := "Up"
	if score < 0 {
		dir = "Down"
	}
	feat["score"] = score
	feat["weight"] = w
	return TA{Score: score, Confidence: conf, Direction: dir, Reason: fmt.Sprintf("delta=%.4f%% %s w=%d", deltaPct, dir, w), Features: feat}
}

func deltaWeight(absDelta float64, cfg config.WindowDelta) int {
	switch {
	case absDelta >= cfg.DeltaStrong*5:
		return 7
	case absDelta >= cfg.DeltaStrong:
		return 5
	case absDelta >= cfg.DeltaWeak:
		return 3
	default:
		return 1
	}
}

func atrSkip(k5m []domain.Candle, cfg config.WindowDelta) (atr, rng float64, skip bool) {
	if cfg.ATRPeriods <= 0 || len(k5m) < 2 {
		return 0, 0, false
	}
	n := cfg.ATRPeriods
	if n > len(k5m)-1 {
		n = len(k5m) - 1
	}
	sum := 0.0
	hist := k5m[:len(k5m)-1]
	if len(hist) > n {
		hist = hist[len(hist)-n:]
	}
	for _, c := range hist {
		sum += c.High - c.Low
	}
	if len(hist) == 0 {
		return 0, 0, false
	}
	atr = sum / float64(len(hist))
	cur := k5m[len(k5m)-1]
	rng = cur.High - cur.Low
	return atr, rng, atr > 0 && rng > atr*cfg.ATRMultiplier
}

func leading(snap domain.MarketSnapshot) (side string, px float64, token string) {
	if snap.PMMidUp >= snap.PMMidDown {
		return "Up", snap.PMMidUp, snap.Market.UpTokenID
	}
	return "Down", snap.PMMidDown, snap.Market.DownTokenID
}
