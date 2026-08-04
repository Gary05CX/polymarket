// Package config loads yaml + environment for the trading bot.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"
)

// Config is the full runtime configuration.
type Config struct {
	Assets     []string         `yaml:"assets"`
	Timeframes []string         `yaml:"timeframes"`
	Binance    BinanceConfig    `yaml:"binance"`
	Gamma      GammaConfig      `yaml:"gamma"`
	CLOB       CLOBConfig       `yaml:"clob"`
	DuckDB     DuckDBConfig     `yaml:"duckdb"`
	Loop       LoopConfig       `yaml:"loop"`
	Discovery  DiscoveryConfig  `yaml:"discovery"`
	FairValue  FairValueConfig  `yaml:"fair_value"`
	StrategyA  StrategyAConfig  `yaml:"strategy_a"`
	StrategyB  StrategyBConfig  `yaml:"strategy_b"`
	Risk       RiskConfig       `yaml:"risk"`
	Monitor    MonitorConfig    `yaml:"monitor"`

	// Secrets / env overlays (not from yaml)
	PrivateKey    string
	CLOBAPIKey    string
	CLOBSecret    string
	CLOBPassphrase string
	Funder        string
	SignatureType int
	WebhookURL    string
}

type BinanceConfig struct {
	WSBase   string            `yaml:"ws_base"`
	Symbols  map[string]string `yaml:"symbols"`
	StaleMs  int               `yaml:"stale_ms"`
}

func (b BinanceConfig) StaleAfter() time.Duration {
	if b.StaleMs <= 0 {
		return 5 * time.Second
	}
	return time.Duration(b.StaleMs) * time.Millisecond
}

type GammaConfig struct {
	BaseURL    string `yaml:"base_url"`
	TimeoutMs  int    `yaml:"timeout_ms"`
}

func (g GammaConfig) Timeout() time.Duration {
	if g.TimeoutMs <= 0 {
		return 8 * time.Second
	}
	return time.Duration(g.TimeoutMs) * time.Millisecond
}

type CLOBConfig struct {
	Host    string `yaml:"host"`
	ChainID int    `yaml:"chain_id"`
	DryRun  bool   `yaml:"dry_run"`
}

type DuckDBConfig struct {
	Path string `yaml:"path"`
}

type LoopConfig struct {
	PollIntervalMs int `yaml:"poll_interval_ms"`
}

func (l LoopConfig) PollInterval() time.Duration {
	if l.PollIntervalMs <= 0 {
		return 1500 * time.Millisecond
	}
	return time.Duration(l.PollIntervalMs) * time.Millisecond
}

type DiscoveryConfig struct {
	CacheTTLMs              int `yaml:"cache_ttl_ms"`
	PreviousWindowGraceSec  int `yaml:"previous_window_grace_sec"`
}

func (d DiscoveryConfig) CacheTTL() time.Duration {
	if d.CacheTTLMs <= 0 {
		return 10 * time.Second
	}
	return time.Duration(d.CacheTTLMs) * time.Millisecond
}

type FairValueConfig struct {
	K            string `yaml:"k"`
	DefaultSigma string `yaml:"default_sigma"`
	PMin         string `yaml:"p_min"`
	PMax         string `yaml:"p_max"`
	VolWindow    int    `yaml:"vol_window"`

	KDec            decimal.Decimal `yaml:"-"`
	DefaultSigmaDec decimal.Decimal `yaml:"-"`
	PMinDec         decimal.Decimal `yaml:"-"`
	PMaxDec         decimal.Decimal `yaml:"-"`
}

type StrategyAConfig struct {
	Enabled         bool   `yaml:"enabled"`
	PriceMin        string `yaml:"price_min"`
	PriceMax        string `yaml:"price_max"`
	MinEdge         string `yaml:"min_edge"`
	SizeMinUSD      string `yaml:"size_min_usd"`
	SizeMaxUSD      string `yaml:"size_max_usd"`
	MinSecondsLeft  int    `yaml:"min_seconds_left"`
	LimitPriceMode  string `yaml:"limit_price_mode"`

	PriceMinDec   decimal.Decimal `yaml:"-"`
	PriceMaxDec   decimal.Decimal `yaml:"-"`
	MinEdgeDec    decimal.Decimal `yaml:"-"`
	SizeMinUSDDec decimal.Decimal `yaml:"-"`
	SizeMaxUSDDec decimal.Decimal `yaml:"-"`
}

type StrategyBConfig struct {
	Enabled             bool   `yaml:"enabled"`
	PriceMoveThreshold  string `yaml:"price_move_threshold"`
	MaxSizeUSD          string `yaml:"max_size_usd"`
	MinEdge             string `yaml:"min_edge"`
	MaxMarketPrice      string `yaml:"max_market_price"`
	MinSecondsLeft      int    `yaml:"min_seconds_left"`
	LimitPriceMode      string `yaml:"limit_price_mode"`

	PriceMoveThresholdDec decimal.Decimal `yaml:"-"`
	MaxSizeUSDDec         decimal.Decimal `yaml:"-"`
	MinEdgeDec            decimal.Decimal `yaml:"-"`
	MaxMarketPriceDec     decimal.Decimal `yaml:"-"`
}

type RiskConfig struct {
	MaxPositionUSDPerMarket string `yaml:"max_position_usd_per_market"`
	MaxOpenMarkets          int    `yaml:"max_open_markets"`
	MaxDailyLossUSD         string `yaml:"max_daily_loss_usd"`
	MaxHourlyLossUSD        string `yaml:"max_hourly_loss_usd"`
	HardMinSecondsLeft      int    `yaml:"hard_min_seconds_left"`

	MaxPositionUSDPerMarketDec decimal.Decimal `yaml:"-"`
	MaxDailyLossUSDDec         decimal.Decimal `yaml:"-"`
	MaxHourlyLossUSDDec        decimal.Decimal `yaml:"-"`
}

type MonitorConfig struct {
	LogLevel string `yaml:"log_level"`
}

// Load reads yaml from path (or CONFIG_PATH), then overlays .env / process env.
func Load(path string) (*Config, error) {
	_ = godotenv.Load() // ignore missing .env

	if path == "" {
		path = os.Getenv("CONFIG_PATH")
	}
	if path == "" {
		path = "configs/config.yaml"
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.parseDecimals(); err != nil {
		return nil, err
	}
	cfg.applyEnv()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) parseDecimals() error {
	var err error
	parse := func(name, s string, dest *decimal.Decimal) {
		if err != nil {
			return
		}
		if strings.TrimSpace(s) == "" {
			err = fmt.Errorf("config %s is empty", name)
			return
		}
		d, e := decimal.NewFromString(s)
		if e != nil {
			err = fmt.Errorf("config %s=%q: %w", name, s, e)
			return
		}
		*dest = d
	}

	parse("fair_value.k", c.FairValue.K, &c.FairValue.KDec)
	parse("fair_value.default_sigma", c.FairValue.DefaultSigma, &c.FairValue.DefaultSigmaDec)
	parse("fair_value.p_min", c.FairValue.PMin, &c.FairValue.PMinDec)
	parse("fair_value.p_max", c.FairValue.PMax, &c.FairValue.PMaxDec)

	parse("strategy_a.price_min", c.StrategyA.PriceMin, &c.StrategyA.PriceMinDec)
	parse("strategy_a.price_max", c.StrategyA.PriceMax, &c.StrategyA.PriceMaxDec)
	parse("strategy_a.min_edge", c.StrategyA.MinEdge, &c.StrategyA.MinEdgeDec)
	parse("strategy_a.size_min_usd", c.StrategyA.SizeMinUSD, &c.StrategyA.SizeMinUSDDec)
	parse("strategy_a.size_max_usd", c.StrategyA.SizeMaxUSD, &c.StrategyA.SizeMaxUSDDec)

	parse("strategy_b.price_move_threshold", c.StrategyB.PriceMoveThreshold, &c.StrategyB.PriceMoveThresholdDec)
	parse("strategy_b.max_size_usd", c.StrategyB.MaxSizeUSD, &c.StrategyB.MaxSizeUSDDec)
	parse("strategy_b.min_edge", c.StrategyB.MinEdge, &c.StrategyB.MinEdgeDec)
	parse("strategy_b.max_market_price", c.StrategyB.MaxMarketPrice, &c.StrategyB.MaxMarketPriceDec)

	parse("risk.max_position_usd_per_market", c.Risk.MaxPositionUSDPerMarket, &c.Risk.MaxPositionUSDPerMarketDec)
	parse("risk.max_daily_loss_usd", c.Risk.MaxDailyLossUSD, &c.Risk.MaxDailyLossUSDDec)
	parse("risk.max_hourly_loss_usd", c.Risk.MaxHourlyLossUSD, &c.Risk.MaxHourlyLossUSDDec)

	return err
}

func (c *Config) applyEnv() {
	c.PrivateKey = firstNonEmpty(os.Getenv("PRIVATE_KEY"), c.PrivateKey)
	c.CLOBAPIKey = os.Getenv("CLOB_API_KEY")
	c.CLOBSecret = os.Getenv("CLOB_SECRET")
	c.CLOBPassphrase = os.Getenv("CLOB_PASSPHRASE")
	c.Funder = os.Getenv("FUNDER")
	c.WebhookURL = os.Getenv("WEBHOOK_URL")

	if v := os.Getenv("CLOB_HOST"); v != "" {
		c.CLOB.Host = v
	}
	if v := os.Getenv("CHAIN_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CLOB.ChainID = n
		}
	}
	if v := os.Getenv("SIGNATURE_TYPE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.SignatureType = n
		}
	}
	if v := os.Getenv("DRY_RUN"); v != "" {
		c.CLOB.DryRun = strings.EqualFold(v, "true") || v == "1"
	}
}

func (c *Config) validate() error {
	if len(c.Assets) == 0 {
		return fmt.Errorf("assets must not be empty")
	}
	if len(c.Timeframes) == 0 {
		return fmt.Errorf("timeframes must not be empty")
	}
	for _, tf := range c.Timeframes {
		if tf != "5m" && tf != "15m" {
			return fmt.Errorf("unsupported timeframe %q (use 5m or 15m)", tf)
		}
	}
	if c.DuckDB.Path == "" {
		return fmt.Errorf("duckdb.path is required")
	}
	if c.CLOB.Host == "" {
		c.CLOB.Host = "https://clob.polymarket.com"
	}
	if c.CLOB.ChainID == 0 {
		c.CLOB.ChainID = 137
	}
	if c.Gamma.BaseURL == "" {
		c.Gamma.BaseURL = "https://gamma-api.polymarket.com"
	}
	if c.Binance.WSBase == "" {
		c.Binance.WSBase = "wss://stream.binance.com:9443/ws"
	}
	if c.Binance.Symbols == nil {
		c.Binance.Symbols = map[string]string{
			"btc": "btcusdt",
			"eth": "ethusdt",
			"sol": "solusdt",
		}
	}
	if !c.CLOB.DryRun && c.PrivateKey == "" {
		return fmt.Errorf("PRIVATE_KEY required when clob.dry_run is false")
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
