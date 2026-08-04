// Package fairvalue estimates simple P(up)/P(down) for short crypto windows.
package fairvalue

import (
	"math"
	"time"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

// Estimate holds fair probabilities and edges vs market.
type Estimate struct {
	PUp     decimal.Decimal
	PDown   decimal.Decimal
	EdgeUp  decimal.Decimal // PUp - marketMidUp
	EdgeDown decimal.Decimal
	Move    decimal.Decimal // (spot-open)/open
	Sigma   decimal.Decimal
}

// Compute returns a basic fair value from open, spot, remaining time, and sigma.
//
//	score ≈ move / (σ * sqrt(t_frac))
//	P_up  = clamp(0.5 + 0.5 * tanh(k * score), p_min, p_max)
func Compute(
	cfg config.FairValueConfig,
	open, spot, midUp, midDown decimal.Decimal,
	sigma decimal.Decimal,
	windowSec int64,
	secondsLeft float64,
) Estimate {
	out := Estimate{}
	if open.IsZero() || spot.IsZero() || windowSec <= 0 {
		// uninformative prior
		out.PUp = decimal.NewFromFloat(0.5)
		out.PDown = decimal.NewFromFloat(0.5)
		return out
	}

	move := spot.Sub(open).Div(open)
	out.Move = move

	if sigma.IsZero() || sigma.IsNegative() {
		sigma = cfg.DefaultSigmaDec
	}
	if sigma.IsZero() {
		sigma = decimal.NewFromFloat(0.0015)
	}
	out.Sigma = sigma

	tFrac := secondsLeft / float64(windowSec)
	if tFrac < 0.02 {
		tFrac = 0.02
	}
	if tFrac > 1 {
		tFrac = 1
	}

	// score = move / (sigma * sqrt(t))
	denom := sigma.InexactFloat64() * math.Sqrt(tFrac)
	if denom < 1e-12 {
		denom = 1e-12
	}
	score := move.InexactFloat64() / denom
	k := cfg.KDec.InexactFloat64()
	if k == 0 {
		k = 2.5
	}
	p := 0.5 + 0.5*math.Tanh(k*score)

	pMin := cfg.PMinDec.InexactFloat64()
	pMax := cfg.PMaxDec.InexactFloat64()
	if pMin <= 0 {
		pMin = 0.02
	}
	if pMax <= 0 || pMax > 1 {
		pMax = 0.98
	}
	if p < pMin {
		p = pMin
	}
	if p > pMax {
		p = pMax
	}

	out.PUp = decimal.NewFromFloat(p).Round(4)
	out.PDown = decimal.NewFromInt(1).Sub(out.PUp)
	if !midUp.IsZero() {
		out.EdgeUp = out.PUp.Sub(midUp)
	}
	if !midDown.IsZero() {
		out.EdgeDown = out.PDown.Sub(midDown)
	}
	return out
}

// SecondsLeft until end (0 if past).
func SecondsLeft(end time.Time, now time.Time) float64 {
	if end.IsZero() {
		return 0
	}
	d := end.Sub(now).Seconds()
	if d < 0 {
		return 0
	}
	return d
}
