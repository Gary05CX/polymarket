package strategy

import (
	"github.com/shopspring/decimal"
)

// Side is the outcome token side.
type Side string

const (
	SideUp   Side = "Up"
	SideDown Side = "Down"
)

// Signal is a proposed limit buy.
type Signal struct {
	Strategy   string // "A" or "B"
	MarketSlug string
	Asset      string
	Timeframe  string
	TokenID    string
	Outcome    Side
	// Limit price for BUY (share price 0-1)
	Price decimal.Decimal
	// Notional in USD
	SizeUSD decimal.Decimal
	// Shares = SizeUSD / Price
	Size decimal.Decimal
	// Diagnostic
	MarketMid  decimal.Decimal
	Fair       decimal.Decimal
	Edge       decimal.Decimal
	SpotMove   decimal.Decimal
	Reason     string
	SecondsLeft float64
}

// MarketInput is the snapshot needed to evaluate strategies.
type MarketInput struct {
	Slug        string
	Asset       string
	Timeframe   string
	UpTokenID   string
	DownTokenID string
	MidUp       decimal.Decimal
	MidDown     decimal.Decimal
	BestBidUp   decimal.Decimal
	BestAskUp   decimal.Decimal
	BestBidDown decimal.Decimal
	BestAskDown decimal.Decimal
	OpenPrice   decimal.Decimal
	Spot        decimal.Decimal
	FairUp      decimal.Decimal
	FairDown    decimal.Decimal
	EdgeUp      decimal.Decimal
	EdgeDown    decimal.Decimal
	SecondsLeft float64
	// existing exposure on this market (USD)
	PositionUSD decimal.Decimal
}
