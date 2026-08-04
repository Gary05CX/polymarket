// Package monitor provides structured logging and optional webhooks.
package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Gary05CX/polymarket/internal/store"
)

// Monitor logs to slog + DuckDB and optionally POSTs to a webhook.
type Monitor struct {
	log        *slog.Logger
	store      *store.Store
	webhookURL string
	http       *http.Client
}

// New creates a monitor.
func New(level string, st *store.Store, webhookURL string) *Monitor {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	return &Monitor{
		log:        slog.New(handler),
		store:      st,
		webhookURL: webhookURL,
		http:       &http.Client{Timeout: 5 * time.Second},
	}
}

// Logger returns the slog logger.
func (m *Monitor) Logger() *slog.Logger { return m.log }

// Info logs and persists.
func (m *Monitor) Info(ctx context.Context, event string, detail any) {
	m.emit(ctx, "info", event, detail)
}

// Warn logs and persists.
func (m *Monitor) Warn(ctx context.Context, event string, detail any) {
	m.emit(ctx, "warn", event, detail)
}

// Error logs, persists, and may webhook.
func (m *Monitor) Error(ctx context.Context, event string, detail any) {
	m.emit(ctx, "error", event, detail)
	m.notify(ctx, event, detail)
}

// Critical is for circuit breakers etc.
func (m *Monitor) Critical(ctx context.Context, event string, detail any) {
	m.emit(ctx, "error", event, detail)
	m.notify(ctx, event, detail)
}

func (m *Monitor) emit(ctx context.Context, level, event string, detail any) {
	ds := stringify(detail)
	switch level {
	case "warn":
		m.log.Warn(event, "detail", ds)
	case "error":
		m.log.Error(event, "detail", ds)
	default:
		m.log.Info(event, "detail", ds)
	}
	if m.store != nil {
		_ = m.store.LogEvent(ctx, level, event, ds)
	}
}

func (m *Monitor) notify(ctx context.Context, event string, detail any) {
	if m.webhookURL == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"event":     event,
		"detail":    detail,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.webhookURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		m.log.Warn("webhook failed", "err", err)
		return
	}
	_ = resp.Body.Close()
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}
