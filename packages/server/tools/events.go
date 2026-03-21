package tools

// events — push notifications to n8n via webhook.
//
// EmitEvent is non-blocking (goroutine) and silently drops if N8N_WEBHOOK_URL
// is not set, so callers never need to guard against it.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/caboose-mcp/server/config"
)

// Event is the payload sent to N8N_WEBHOOK_URL on each push notification.
type Event struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Timestamp time.Time      `json:"ts"`
	Source    string         `json:"source"` // always "fafb"
	Data      map[string]any `json:"data"`
}

// EmitEvent fires a webhook POST to N8N_WEBHOOK_URL.
// Always non-blocking (goroutine). Silently drops if URL not set.
func EmitEvent(cfg *config.Config, ev Event) {
	if cfg.N8nWebhookURL == "" {
		return
	}
	if ev.Source == "" {
		ev.Source = "fafb"
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	go func() {
		body, err := json.Marshal(ev)
		if err != nil {
			return
		}
		req, err := http.NewRequest("POST", cfg.N8nWebhookURL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.N8nAPIKey != "" {
			req.Header.Set("X-N8N-API-KEY", cfg.N8nAPIKey)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}
