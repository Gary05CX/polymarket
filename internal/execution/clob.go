package execution

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/0xNetuser/Polymarket-golang/polymarket"
	"github.com/Gary05CX/polymarket/internal/config"
	"github.com/shopspring/decimal"
)

// LiveCLOB wraps 0xNetuser/Polymarket-golang V2 client.
type LiveCLOB struct {
	client *polymarket.ClobClient
	log    *slog.Logger
}

// NewLiveCLOB creates an authenticated CLOB client when not dry-run.
// When dry_run is true, returns a NoopCLOB.
func NewLiveCLOB(cfg *config.Config, log *slog.Logger) (CLOB, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.CLOB.DryRun {
		log.Info("CLOB dry_run enabled — orders will not be posted")
		return &NoopCLOB{log: log}, nil
	}

	host := cfg.CLOB.Host
	chainID := cfg.CLOB.ChainID
	pk := cfg.PrivateKey

	var sigType *int
	if cfg.SignatureType != 0 {
		st := cfg.SignatureType
		sigType = &st
	}
	funder := cfg.Funder

	var creds *polymarket.ApiCreds
	if cfg.CLOBAPIKey != "" && cfg.CLOBSecret != "" && cfg.CLOBPassphrase != "" {
		creds = &polymarket.ApiCreds{
			APIKey:        cfg.CLOBAPIKey,
			APISecret:     cfg.CLOBSecret,
			APIPassphrase: cfg.CLOBPassphrase,
		}
	}

	client, err := polymarket.NewClobClient(host, chainID, pk, creds, sigType, funder)
	if err != nil {
		return nil, fmt.Errorf("new clob client: %w", err)
	}

	// Derive API key if not provided
	if creds == nil {
		derived, err := client.CreateOrDeriveAPIKey(nil)
		if err != nil {
			return nil, fmt.Errorf("derive api key: %w", err)
		}
		client.SetAPICreds(derived)
		log.Info("derived CLOB API credentials")
	}

	return &LiveCLOB{client: client, log: log}, nil
}

// GetBookMid implements CLOB.
func (c *LiveCLOB) GetBookMid(ctx context.Context, tokenID string) (OrderBookMid, error) {
	_ = ctx
	book, err := c.client.GetOrderBook(tokenID)
	if err != nil {
		return OrderBookMid{}, err
	}
	return bookToMid(book), nil
}

func bookToMid(book *polymarket.OrderBookSummary) OrderBookMid {
	var out OrderBookMid
	if book == nil {
		return out
	}
	if len(book.Bids) > 0 {
		out.Bid = parseDec(book.Bids[0].Price)
		for _, b := range book.Bids {
			p := parseDec(b.Price)
			if p.GreaterThan(out.Bid) {
				out.Bid = p
			}
		}
	}
	if len(book.Asks) > 0 {
		out.Ask = parseDec(book.Asks[0].Price)
		for _, a := range book.Asks {
			p := parseDec(a.Price)
			if out.Ask.IsZero() || p.LessThan(out.Ask) {
				out.Ask = p
			}
		}
	}
	if !out.Bid.IsZero() && !out.Ask.IsZero() {
		out.Mid = out.Bid.Add(out.Ask).Div(decimal.NewFromInt(2))
	} else if !out.Bid.IsZero() {
		out.Mid = out.Bid
	} else {
		out.Mid = out.Ask
	}
	return out
}

// PlaceLimitBuy implements CLOB.
func (c *LiveCLOB) PlaceLimitBuy(ctx context.Context, tokenID string, price, size decimal.Decimal, postOnly bool) (string, error) {
	_ = ctx
	resp, err := c.client.CreateAndPostOrderV2(
		&polymarket.OrderArgsV2{
			TokenID: tokenID,
			Price:   price.InexactFloat64(),
			Size:    size.InexactFloat64(),
			Side:    polymarket.BUY,
		},
		nil,
		polymarket.OrderTypeGTC,
		postOnly,
		false,
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("nil order response")
	}
	id := extractOrderIDFromPost(resp)
	if id == "" {
		return "", fmt.Errorf("order response missing id: %+v", resp)
	}
	return id, nil
}

// CancelOrder implements CLOB.
func (c *LiveCLOB) CancelOrder(ctx context.Context, orderID string) error {
	_ = ctx
	_, err := c.client.Cancel(orderID)
	return err
}

// CancelAll implements CLOB.
func (c *LiveCLOB) CancelAll(ctx context.Context) error {
	_ = ctx
	_, err := c.client.CancelAll()
	return err
}

func parseDec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		f, err2 := strconv.ParseFloat(s, 64)
		if err2 != nil {
			return decimal.Zero
		}
		return decimal.NewFromFloat(f)
	}
	return d
}

func extractOrderIDFromPost(resp *polymarket.PostOrderResultV2) string {
	if resp == nil {
		return ""
	}
	// Prefer Response map
	if m, ok := resp.Response.(map[string]interface{}); ok {
		if id := pickID(m); id != "" {
			return id
		}
	}
	if resp.Payload != nil {
		if id := pickID(resp.Payload); id != "" {
			return id
		}
	}
	// Response might be nested
	if s, ok := resp.Response.(string); ok && s != "" {
		return s
	}
	return ""
}

func pickID(m map[string]interface{}) string {
	for _, k := range []string{"orderID", "orderId", "order_id", "id", "ID"} {
		if v, ok := m[k]; ok && v != nil {
			s := fmt.Sprint(v)
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// NoopCLOB is used in dry-run mode (no network trading).
type NoopCLOB struct {
	log *slog.Logger
}

func (n *NoopCLOB) GetBookMid(ctx context.Context, tokenID string) (OrderBookMid, error) {
	_ = ctx
	_ = tokenID
	return OrderBookMid{}, fmt.Errorf("noop: no orderbook in pure dry-run; use gamma mids")
}

func (n *NoopCLOB) PlaceLimitBuy(ctx context.Context, tokenID string, price, size decimal.Decimal, postOnly bool) (string, error) {
	_ = ctx
	return "", fmt.Errorf("noop clob: dry_run should not call PlaceLimitBuy")
}

func (n *NoopCLOB) CancelOrder(ctx context.Context, orderID string) error {
	_ = ctx
	_ = orderID
	return nil
}

func (n *NoopCLOB) CancelAll(ctx context.Context) error {
	_ = ctx
	return nil
}

// Ensure interfaces compile.
var (
	_ CLOB = (*LiveCLOB)(nil)
	_ CLOB = (*NoopCLOB)(nil)
)
