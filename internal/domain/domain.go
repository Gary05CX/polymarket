package domain

import "time"

type Candle struct {
	OpenTime time.Time
	Open     float64
	High     float64
	Low      float64
	Close    float64
	Volume   float64
}

type BookLevel struct {
	Price float64
	Size  float64
}

type Book struct {
	Bids         []BookLevel
	Asks         []BookLevel
	MinOrderSize float64
	TickSize     float64
}

type Market struct {
	Slug             string
	Asset            string
	ConditionID      string
	WindowStart      time.Time
	WindowEnd        time.Time
	UpTokenID        string
	DownTokenID      string
	Outcomes         []string
	MinOrderSize     float64
	TickSize         float64
	FeeRate          float64
	SecondsDelay     float64
	ResolutionSource string
	Closed           bool
	OutcomePrices    []float64
}

type MarketSnapshot struct {
	Now          time.Time
	Market       Market
	SecondsLeft  float64
	Elapsed      float64
	PMMidUp      float64
	PMMidDown    float64
	Book         Book
	BinancePrice float64
	WindowOpenPx float64
	Klines1m     []Candle
	Klines5m     []Candle
}

type Signal struct {
	StrategyID     string
	StrategySource string
	Asset          string
	MarketSlug     string
	ConditionID    string
	Side           string
	TokenID        string
	Confidence     float64
	Score          float64
	Features       map[string]any
	Reason         string
	ReasonCode     string
	PMPrice        float64
	BinancePrice   float64
	WindowOpenPx   float64
	SecondsLeft    float64
}

type Reject struct {
	ReasonCode string
	Reason     string
	Features   map[string]any
	Slug       string
}

type Order struct {
	ID             string
	RunID          string
	StrategyID     string
	StrategySource string
	RunMode        string
	MarketSlug     string
	Side           string
	TokenID        string
	IntendedPrice  float64
	IntendedShares float64
	LimitPrice     float64
	NotionalUSDC   float64
	IsMinSize      bool
	Status         string
}

type Fill struct {
	ID            string
	OrderID       string
	RunID         string
	MarketSlug    string
	FillModel     string
	Price         float64
	Shares        float64
	FeeUSDC       float64
	FeeRate       float64
	LiquidityFlag string
}
