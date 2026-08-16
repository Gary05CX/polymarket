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
	Database   DatabaseConfig   `yaml:"database"`
	DuckDB     DuckDBConfig     `yaml:"duckdb"` // legacy; prefer database.*
	Loop       LoopConfig       `yaml:"loop"`
	Discovery  DiscoveryConfig  `yaml:"discovery"`
	FairValue  FairValueConfig  `yaml:"fair_value"`
	StrategyA  StrategyAConfig  `yaml:"strategy_a"`
	StrategyB  StrategyBConfig  `yaml:"strategy_b"`
	StrategyC  StrategyCConfig  `yaml:"strategy_c"` // manual 70% momentum (optional)
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
	DryRun  bool   `yaml:"dry_run"` // env DRY_RUN overrides; keep true until official-settle paper looks good
}

// DatabaseConfig selects backend: duckdb (default) or postgres.
type DatabaseConfig struct {
	// Driver: duckdb | postgres (overridden by env DB_DRIVER)
	Driver string `yaml:"driver"`
	// DuckDBPath file path (overridden by DUCKDB_PATH)
	DuckDBPath string `yaml:"duckdb_path"`
	// PostgresURL e.g. postgres://user:pass@127.0.0.1:5432/polymarket?sslmode=disable
	// (overridden by DATABASE_URL)
	PostgresURL string `yaml:"postgres_url"`
}

type DuckDBConfig struct {
	Path string `yaml:"path"`
}

type LoopConfig struct {
	PollIntervalMs int `yaml:"poll_interval_ms"`
	// SnapshotEveryNTicks: write price_snapshots every N polls (1 = every tick).
	SnapshotEveryNTicks int `yaml:"snapshot_every_n_ticks"`
	// RejectSummaryEveryNTicks: log aggregated risk rejects every N polls (0 = off).
	RejectSummaryEveryNTicks int `yaml:"reject_summary_every_n_ticks"`
	// OpenPriceMaxAgeSec: only lock open_price when first seen within this many
	// seconds after window_start (0 = always first sighting).
	OpenPriceMaxAgeSec int `yaml:"open_price_max_age_sec"`
	// CheckpointEveryNTicks: run DuckDB CHECKPOINT every N polls (0 = off).
	CheckpointEveryNTicks int `yaml:"checkpoint_every_n_ticks"`
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
	Enabled        bool   `yaml:"enabled"`
	PriceMin       string `yaml:"price_min"`
	PriceMax       string `yaml:"price_max"`
	MinEdge        string `yaml:"min_edge"`
	// MaxEdge rejects inflated edges (often fair stuck at p_max). Empty = no cap.
	MaxEdge string `yaml:"max_edge"`
	// EdgeSlope: required_edge = min_edge + max(0, mid-price_min)*edge_slope
	// Higher mid (worse payoff) needs more edge. 0 = flat min_edge only.
	EdgeSlope string `yaml:"edge_slope"`
	// RejectFairAtClamp: skip when fair is pinned at p_min/p_max.
	RejectFairAtClamp bool   `yaml:"reject_fair_at_clamp"`
	SizeMinUSD        string `yaml:"size_min_usd"`
	SizeMaxUSD        string `yaml:"size_max_usd"`
	MinSecondsLeft    int    `yaml:"min_seconds_left"`
	LimitPriceMode    string `yaml:"limit_price_mode"`
	// PaperOnly: never send to CLOB even when DRY_RUN=false (data collection).
	PaperOnly bool `yaml:"paper_only"`

	PriceMinDec   decimal.Decimal `yaml:"-"`
	PriceMaxDec   decimal.Decimal `yaml:"-"`
	MinEdgeDec    decimal.Decimal `yaml:"-"`
	MaxEdgeDec    decimal.Decimal `yaml:"-"`
	EdgeSlopeDec  decimal.Decimal `yaml:"-"`
	SizeMinUSDDec decimal.Decimal `yaml:"-"`
	SizeMaxUSDDec decimal.Decimal `yaml:"-"`
}

type StrategyBConfig struct {
	Enabled            bool   `yaml:"enabled"`
	PriceMoveThreshold string `yaml:"price_move_threshold"`
	MaxSizeUSD         string `yaml:"max_size_usd"`
	MinEdge            string `yaml:"min_edge"`
	MaxMarketPrice     string `yaml:"max_market_price"`
	MinSecondsLeft     int    `yaml:"min_seconds_left"`
	LimitPriceMode     string `yaml:"limit_price_mode"`
	// RejectFairAtClamp: same as A — skip if fair pinned at bounds.
	RejectFairAtClamp bool `yaml:"reject_fair_at_clamp"`
	// PaperOnly: never send to CLOB even when DRY_RUN=false (data collection).
	PaperOnly bool `yaml:"paper_only"`

	PriceMoveThresholdDec decimal.Decimal `yaml:"-"`
	MaxSizeUSDDec         decimal.Decimal `yaml:"-"`
	MinEdgeDec            decimal.Decimal `yaml:"-"`
	MaxMarketPriceDec     decimal.Decimal `yaml:"-"`
}

// StrategyCConfig is the manual momentum profile (distance from open + leading side).
type StrategyCConfig struct {
	Enabled bool `yaml:"enabled"`
	// SizeUSD fixed notional per order (e.g. "5").
	SizeUSD string `yaml:"size_usd"`
	// MinElapsedSec: only start counting the move after market open this long
	// (e.g. 60 = first minute of the 5m window is ignored for sustain).
	MinElapsedSec int `yaml:"min_elapsed_sec"`
	// SustainedAboveSec: |spot-open| must stay above thr continuously this long
	// after MinElapsedSec (e.g. 120 = 2 minutes). Earliest entry ≈ min+sustained.
	SustainedAboveSec int `yaml:"sustained_above_sec"`
	// Move thresholds in USD vs window open/target (strictly greater than).
	BTCMoveUSD string `yaml:"btc_move_usd"` // e.g. "2"
	ETHMoveUSD string `yaml:"eth_move_usd"` // e.g. "0.2"
	// Mid band for the leading side (optional filter; widen to trade more often).
	MidMin string `yaml:"mid_min"`
	MidMax string `yaml:"mid_max"`
	MinSecondsLeft int    `yaml:"min_seconds_left"`
	LimitPriceMode string `yaml:"limit_price_mode"`
	// Stability: if price is retracing toward target, wait then re-check.
	StabilityWaitSec     int    `yaml:"stability_wait_sec"`     // e.g. 45 (30–60)
	StabilityLookbackSec int    `yaml:"stability_lookback_sec"` // compare abs-move vs this many seconds ago
	// RetraceEpsilonUSD: abs-move must fall by more than this to count as retracing.
	RetraceEpsilonUSD string `yaml:"retrace_epsilon_usd"`
	// After this many consecutive settled losses, pause new C signals.
	MaxConsecutiveLosses int `yaml:"max_consecutive_losses"`
	CooldownSec              int `yaml:"cooldown_sec"` // e.g. 2700 = 45m
	// PaperOnly: never send to CLOB even when DRY_RUN=false (data collection).
	PaperOnly bool `yaml:"paper_only"`

	SizeUSDDec           decimal.Decimal `yaml:"-"`
	BTCMoveUSDDec        decimal.Decimal `yaml:"-"`
	ETHMoveUSDDec        decimal.Decimal `yaml:"-"`
	MidMinDec            decimal.Decimal `yaml:"-"`
	MidMaxDec            decimal.Decimal `yaml:"-"`
	RetraceEpsilonUSDDec decimal.Decimal `yaml:"-"`
}

type RiskConfig struct {
	MaxPositionUSDPerMarket string `yaml:"max_position_usd_per_market"`
	MaxOpenMarkets          int    `yaml:"max_open_markets"`
	MaxDailyLossUSD         string `yaml:"max_daily_loss_usd"`
	MaxHourlyLossUSD        string `yaml:"max_hourly_loss_usd"`
	HardMinSecondsLeft      int    `yaml:"hard_min_seconds_left"`
	// OneOrderPerStrategy: at most one order per market×strategy per window (default true).
	OneOrderPerStrategy bool `yaml:"one_order_per_strategy"`
	// OneOrderPerMarket: at most one order total per market window (A and B mutually exclusive).
	OneOrderPerMarket bool   `yaml:"one_order_per_market"`
	MaxSpread         string `yaml:"max_spread"`
	// PaperFillAtMid: rewrite limit price to mid for paper PnL (dry_run only).
	PaperFillAtMid bool `yaml:"paper_fill_at_mid"`
	// PaperFeeBps: subtract fee from paper PnL on settle (e.g. 200 = 2%).
	PaperFeeBps int `yaml:"paper_fee_bps"`

	MaxPositionUSDPerMarketDec decimal.Decimal `yaml:"-"`
	MaxDailyLossUSDDec         decimal.Decimal `yaml:"-"`
	MaxHourlyLossUSDDec        decimal.Decimal `yaml:"-"`
	MaxSpreadDec               decimal.Decimal `yaml:"-"`
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
	// optional
	if strings.TrimSpace(c.StrategyA.MaxEdge) != "" {
		parse("strategy_a.max_edge", c.StrategyA.MaxEdge, &c.StrategyA.MaxEdgeDec)
	}
	if strings.TrimSpace(c.StrategyA.EdgeSlope) == "" {
		c.StrategyA.EdgeSlope = "0"
	}
	parse("strategy_a.edge_slope", c.StrategyA.EdgeSlope, &c.StrategyA.EdgeSlopeDec)

	parse("strategy_b.price_move_threshold", c.StrategyB.PriceMoveThreshold, &c.StrategyB.PriceMoveThresholdDec)
	parse("strategy_b.max_size_usd", c.StrategyB.MaxSizeUSD, &c.StrategyB.MaxSizeUSDDec)
	parse("strategy_b.min_edge", c.StrategyB.MinEdge, &c.StrategyB.MinEdgeDec)
	parse("strategy_b.max_market_price", c.StrategyB.MaxMarketPrice, &c.StrategyB.MaxMarketPriceDec)

	// Strategy C optional — only required when enabled.
	if c.StrategyC.Enabled {
		if c.StrategyC.MinElapsedSec <= 0 {
			c.StrategyC.MinElapsedSec = 60 // first minute: do not start sustain clock
		}
		if c.StrategyC.SustainedAboveSec <= 0 {
			c.StrategyC.SustainedAboveSec = 120 // hold thr for 2 minutes
		}
		if c.StrategyC.StabilityWaitSec <= 0 {
			c.StrategyC.StabilityWaitSec = 45
		}
		if c.StrategyC.StabilityLookbackSec <= 0 {
			c.StrategyC.StabilityLookbackSec = 30
		}
		if c.StrategyC.MaxConsecutiveLosses <= 0 {
			c.StrategyC.MaxConsecutiveLosses = 2
		}
		if c.StrategyC.CooldownSec <= 0 {
			c.StrategyC.CooldownSec = 45 * 60
		}
		if strings.TrimSpace(c.StrategyC.LimitPriceMode) == "" {
			c.StrategyC.LimitPriceMode = "mid"
		}
		if strings.TrimSpace(c.StrategyC.RetraceEpsilonUSD) == "" {
			c.StrategyC.RetraceEpsilonUSD = "0.3"
		}
		parse("strategy_c.size_usd", c.StrategyC.SizeUSD, &c.StrategyC.SizeUSDDec)
		parse("strategy_c.btc_move_usd", c.StrategyC.BTCMoveUSD, &c.StrategyC.BTCMoveUSDDec)
		parse("strategy_c.eth_move_usd", c.StrategyC.ETHMoveUSD, &c.StrategyC.ETHMoveUSDDec)
		parse("strategy_c.mid_min", c.StrategyC.MidMin, &c.StrategyC.MidMinDec)
		parse("strategy_c.mid_max", c.StrategyC.MidMax, &c.StrategyC.MidMaxDec)
		parse("strategy_c.retrace_epsilon_usd", c.StrategyC.RetraceEpsilonUSD, &c.StrategyC.RetraceEpsilonUSDDec)
	}

	parse("risk.max_position_usd_per_market", c.Risk.MaxPositionUSDPerMarket, &c.Risk.MaxPositionUSDPerMarketDec)
	parse("risk.max_daily_loss_usd", c.Risk.MaxDailyLossUSD, &c.Risk.MaxDailyLossUSDDec)
	parse("risk.max_hourly_loss_usd", c.Risk.MaxHourlyLossUSD, &c.Risk.MaxHourlyLossUSDDec)
	if strings.TrimSpace(c.Risk.MaxSpread) == "" {
		c.Risk.MaxSpread = "0.05"
	}
	parse("risk.max_spread", c.Risk.MaxSpread, &c.Risk.MaxSpreadDec)

	// defaults for new risk flags (yaml false is zero value — treat unset carefully)
	// OneOrderPerStrategy defaults to true unless explicitly false in yaml with key present;
	// we enable by default after unmarshal if the field was omitted by checking via pointer is hard —
	// so default true here when loading: if yaml has one_order_per_strategy: false it stays false.
	// Problem: zero value is false. Use a post-default: we set default true in config.yaml always.
	// Also enable paper_fill_at_mid default true in yaml.

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

	// Database backend switch (env wins over yaml)
	if v := strings.TrimSpace(os.Getenv("DB_DRIVER")); v != "" {
		c.Database.Driver = v
	}
	if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		c.Database.PostgresURL = v
	}
	if v := strings.TrimSpace(os.Getenv("DUCKDB_PATH")); v != "" {
		c.Database.DuckDBPath = v
		c.DuckDB.Path = v
	}
	// legacy alias
	if v := strings.TrimSpace(os.Getenv("POSTGRES_URL")); v != "" && c.Database.PostgresURL == "" {
		c.Database.PostgresURL = v
	}
}

// EffectiveDryRun is true when this strategy must not hit the CLOB.
// Global DRY_RUN=true papers everything; paper_only papers one strategy
// even if DRY_RUN=false (used so B/C keep collecting while A trades live).
func (c *Config) EffectiveDryRun(strategy string) bool {
	if c == nil || c.CLOB.DryRun {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(strategy)) {
	case "A":
		return c.StrategyA.PaperOnly
	case "B":
		return c.StrategyB.PaperOnly
	case "C":
		return c.StrategyC.PaperOnly
	default:
		return true
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
	// Normalize database config (yaml database.* + legacy duckdb.path)
	if c.Database.DuckDBPath == "" && c.DuckDB.Path != "" {
		c.Database.DuckDBPath = c.DuckDB.Path
	}
	if c.Database.DuckDBPath == "" {
		c.Database.DuckDBPath = "data/bot.duckdb"
	}
	if c.DuckDB.Path == "" {
		c.DuckDB.Path = c.Database.DuckDBPath
	}
	drv := strings.ToLower(strings.TrimSpace(c.Database.Driver))
	if drv == "" {
		drv = "duckdb"
	}
	if drv == "postgresql" || drv == "pg" {
		drv = "postgres"
	}
	c.Database.Driver = drv
	switch drv {
	case "duckdb":
		// ok
	case "postgres":
		if strings.TrimSpace(c.Database.PostgresURL) == "" {
			return fmt.Errorf("database.postgres_url or DATABASE_URL required when DB_DRIVER=postgres")
		}
	default:
		return fmt.Errorf("unsupported database.driver %q (use duckdb or postgres)", c.Database.Driver)
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
