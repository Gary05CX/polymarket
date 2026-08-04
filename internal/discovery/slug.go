package discovery

import (
	"fmt"
	"strings"
	"time"
)

// TimeframeSeconds returns window length in seconds.
func TimeframeSeconds(tf string) (int64, error) {
	switch strings.ToLower(tf) {
	case "5m", "5min", "5":
		return 300, nil
	case "15m", "15min", "15":
		return 900, nil
	default:
		return 0, fmt.Errorf("unknown timeframe %q", tf)
	}
}

// NormalizeTimeframe maps config values to canonical 5m / 15m.
func NormalizeTimeframe(tf string) string {
	sec, err := TimeframeSeconds(tf)
	if err != nil {
		return tf
	}
	if sec == 300 {
		return "5m"
	}
	return "15m"
}

// WindowStart floors unix time to the timeframe boundary.
func WindowStart(now time.Time, timeframeSec int64) int64 {
	ts := now.UTC().Unix()
	return ts - (ts % timeframeSec)
}

// WindowStartFor returns window start for a named timeframe.
func WindowStartFor(now time.Time, tf string) (int64, int64, error) {
	sec, err := TimeframeSeconds(tf)
	if err != nil {
		return 0, 0, err
	}
	return WindowStart(now, sec), sec, nil
}

// CandidateSlugs builds deterministic slug variants for Gamma lookup.
// Primary live format (verified): {asset}-updown-{5m|15m}-{window_start_ts}
// Longer variants kept as fallbacks in case Polymarket renames.
func CandidateSlugs(asset, timeframe string, windowStart int64) []string {
	asset = strings.ToLower(strings.TrimSpace(asset))
	tf := NormalizeTimeframe(timeframe)

	var shortTF string
	switch tf {
	case "5m":
		shortTF = "5m"
	default:
		shortTF = "15m"
	}

	// asset aliases for longer slug forms
	fullName := asset
	switch asset {
	case "btc":
		fullName = "bitcoin"
	case "eth":
		fullName = "ethereum"
	case "sol":
		fullName = "solana"
	}

	slugs := []string{
		fmt.Sprintf("%s-updown-%s-%d", asset, shortTF, windowStart),
		fmt.Sprintf("%s-up-or-down-%s-%d", asset, shortTF, windowStart),
	}
	if shortTF == "5m" {
		slugs = append(slugs,
			fmt.Sprintf("%s-up-or-down-5-minute-windows-%d", asset, windowStart),
			fmt.Sprintf("%s-up-or-down-5-minute-windows-%d", fullName, windowStart),
		)
	} else {
		slugs = append(slugs,
			fmt.Sprintf("%s-up-or-down-15-minute-windows-%d", asset, windowStart),
			fmt.Sprintf("%s-up-or-down-15-minute-windows-%d", fullName, windowStart),
		)
	}
	// de-dupe while preserving order
	seen := make(map[string]struct{}, len(slugs))
	out := make([]string, 0, len(slugs))
	for _, s := range slugs {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
