package config

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mode       string     `yaml:"mode"`
	Strategy   string     `yaml:"strategy"`
	MaxWindows int        `yaml:"max_windows"`
	Assets     []string   `yaml:"assets"`
	Database   Database   `yaml:"database"`
	Polymarket Polymarket `yaml:"polymarket"`
	Binance    Binance    `yaml:"binance"`
	Clock      Clock      `yaml:"clock"`
	Execution  Execution  `yaml:"execution"`
	Strategies Strategies `yaml:"strategies"`
	Resolver   Resolver   `yaml:"resolver"`
	SQLDir     string     `yaml:"-"`
}

type Database struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type Polymarket struct {
	GammaURL string `yaml:"gamma_url"`
	ClobURL  string `yaml:"clob_url"`
	RTDSURL  string `yaml:"rtds_url"`
	ChainID  int    `yaml:"chain_id"`
}

type Binance struct {
	RestURL string            `yaml:"rest_url"`
	Symbols map[string]string `yaml:"symbols"`
}

type Clock struct {
	Sync              string `yaml:"sync"`
	ResyncIntervalSec int    `yaml:"resync_interval_sec"`
}

type Execution struct {
	UseMinSharesOnly    bool    `yaml:"use_min_shares_only"`
	BudgetUSDCCap       float64 `yaml:"budget_usdc_cap"`
	OrderType           string  `yaml:"order_type"`
	PriceOffset         float64 `yaml:"price_offset"`
	PollBookBeforeOrder bool    `yaml:"poll_book_before_order"`
}

type Strategies struct {
	WindowDelta WindowDelta `yaml:"window_delta"`
	Inertia3m   Inertia3m   `yaml:"inertia_3m"`
}

type WindowDelta struct {
	WakeBeforeSec   int                `yaml:"wake_before_sec"`
	EntrySecondsMin float64            `yaml:"entry_seconds_min"`
	EntrySecondsMax float64            `yaml:"entry_seconds_max"`
	PriceMin        map[string]float64 `yaml:"price_min"`
	PriceMax        float64            `yaml:"price_max"`
	DeltaSkip       float64            `yaml:"delta_skip"`
	DeltaWeak       float64            `yaml:"delta_weak"`
	DeltaStrong     float64            `yaml:"delta_strong"`
	MinConfidence   float64            `yaml:"min_confidence"`
	ATRPeriods      int                `yaml:"atr_periods"`
	ATRMultiplier   float64            `yaml:"atr_multiplier"`
	PollIntervalSec float64            `yaml:"poll_interval_sec"`
}

type Inertia3m struct {
	EntryDelaySec    float64 `yaml:"entry_delay_sec"`
	MinTimeRemaining float64 `yaml:"min_time_remaining"`
	FlatThresholdPct float64 `yaml:"flat_threshold_pct"`
	StrongMovePct    float64 `yaml:"strong_move_pct"`
	MinScoreModerate int     `yaml:"min_score_moderate"`
	MinScoreStrong   int     `yaml:"min_score_strong"`
	MinEdge          float64 `yaml:"min_edge"`
	PollIntervalSec  float64 `yaml:"poll_interval_sec"`
}

type Resolver struct {
	Enabled              bool    `yaml:"enabled"`
	PollAfterCloseSec    float64 `yaml:"poll_after_close_sec"`
	PollIntervalSec      float64 `yaml:"poll_interval_sec"`
	PollTimeoutSec       float64 `yaml:"poll_timeout_sec"`
	StoreProvisionalTWAP bool    `yaml:"store_provisional_twap"`
	TWAPTopic            string  `yaml:"twap_topic"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := defaultConfig()
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	cfg.normalize()
	return cfg, nil
}

func ParseFlags() (cfgPath string, mode, strategy string, maxWindows int) {
	flag.StringVar(&cfgPath, "config", "config.yaml", "config file")
	flag.StringVar(&mode, "mode", "", "dry_run | paper | live")
	flag.StringVar(&strategy, "strategy", "", "window_delta | inertia_3m")
	flag.IntVar(&maxWindows, "max-windows", -1, "override max_windows; -1 = use config")
	flag.Parse()
	return
}

func (c *Config) ApplyOverrides(mode, strategy string, maxWindows int) {
	if mode != "" {
		c.Mode = mode
	}
	if strategy != "" {
		c.Strategy = strategy
	}
	if maxWindows >= 0 {
		c.MaxWindows = maxWindows
	}
	c.normalize()
}

func (c *Config) LiveReady() error {
	if c.Mode != "live" {
		return nil
	}
	if os.Getenv("POLY_PRIVATE_KEY") == "" || os.Getenv("POLY_FUNDER") == "" {
		return fmt.Errorf("live requires POLY_PRIVATE_KEY and POLY_FUNDER")
	}
	return fmt.Errorf("live CLOB V2 not implemented; stay on dry_run")
}

func (c *Config) normalize() {
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode == "paper" {
		c.Mode = "dry_run"
	}
	c.Strategy = strings.TrimSpace(c.Strategy)
	c.Database.Driver = strings.ToLower(strings.TrimSpace(c.Database.Driver))
	if c.SQLDir == "" {
		c.SQLDir = "sql"
	}
	if len(c.Assets) == 0 {
		c.Assets = []string{"BTC"}
	}
	if c.Binance.Symbols == nil {
		c.Binance.Symbols = map[string]string{"BTC": "BTCUSDT", "ETH": "ETHUSDT"}
	}
}

func defaultConfig() *Config {
	return &Config{
		Mode:       "dry_run",
		Strategy:   "window_delta",
		MaxWindows: 1,
		Assets:     []string{"BTC"},
		Database: Database{
			Driver: "postgres",
			DSN:    "postgres:///polymarket_bot?host=/var/run/postgresql",
		},
		Polymarket: Polymarket{
			GammaURL: "https://gamma-api.polymarket.com",
			ClobURL:  "https://clob.polymarket.com",
			RTDSURL:  "wss://ws-live-data.polymarket.com",
			ChainID:  137,
		},
		Binance: Binance{
			RestURL: "https://api.binance.com",
			Symbols: map[string]string{"BTC": "BTCUSDT", "ETH": "ETHUSDT"},
		},
		Clock: Clock{Sync: "binance", ResyncIntervalSec: 30},
		Execution: Execution{
			UseMinSharesOnly:    true,
			BudgetUSDCCap:       20,
			OrderType:           "FOK",
			PriceOffset:         0.01,
			PollBookBeforeOrder: true,
		},
		Strategies: Strategies{
			WindowDelta: WindowDelta{
				WakeBeforeSec:   65,
				EntrySecondsMin: 10,
				EntrySecondsMax: 50,
				PriceMin:        map[string]float64{"BTC": 0.94, "ETH": 0.92},
				PriceMax:        0.99,
				DeltaSkip:       0.0005,
				DeltaWeak:       0.001,
				DeltaStrong:     0.002,
				MinConfidence:   0.30,
				ATRPeriods:      5,
				ATRMultiplier:   1.5,
				PollIntervalSec: 3,
			},
			Inertia3m: Inertia3m{
				EntryDelaySec:    170,
				MinTimeRemaining: 60,
				FlatThresholdPct: 0.05,
				StrongMovePct:    0.10,
				MinScoreModerate: 4,
				MinScoreStrong:   5,
				MinEdge:          0.10,
				PollIntervalSec:  10,
			},
		},
		Resolver: Resolver{
			Enabled:           true,
			PollAfterCloseSec: 30,
			PollIntervalSec:   15,
			PollTimeoutSec:    900,
			TWAPTopic:         "auto",
		},
		SQLDir: "sql",
	}
}
