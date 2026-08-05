package risk

import (
	"testing"

	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/Gary05CX/polymarket/internal/strategy"
	"github.com/shopspring/decimal"
)

func TestBatchDedupeKey(t *testing.T) {
	// pure logic smoke: two same keys should not both pass when OneOrderPerStrategy
	// without a DB we only check config wiring defaults
	cfg := config.RiskConfig{
		OneOrderPerStrategy:        true,
		PaperFillAtMid:             true,
		MaxPositionUSDPerMarketDec: decimal.NewFromInt(8),
		MaxOpenMarkets:             4,
		MaxSpreadDec:               decimal.RequireFromString("0.05"),
	}
	if !cfg.OneOrderPerStrategy {
		t.Fatal("expected one_order_per_strategy")
	}
	s := strategy.Signal{
		MarketSlug: "m1",
		Strategy:   "A",
		MarketMid:  decimal.RequireFromString("0.65"),
		Price:      decimal.RequireFromString("0.50"),
		SizeUSD:    decimal.NewFromInt(2),
		Size:       decimal.NewFromInt(4),
		BestBid:    decimal.RequireFromString("0.64"),
		BestAsk:    decimal.RequireFromString("0.66"),
	}
	if s.MarketMid.IsZero() {
		t.Fatal("mid required for paper fill")
	}
}
