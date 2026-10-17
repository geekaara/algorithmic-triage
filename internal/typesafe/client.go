// Package typesafe calls a System One decision model (such as TypeSafe's
// Jev) through Vercel AI Gateway's TypeSafe-compatible API. A decision
// model answers typed questions about some state: it picks a choice, gives
// a score, or a yes/no probability, always with calibrated probabilities.
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://ai-gateway.vercel.sh/typesafe"

type Client struct {
	baseURL string
	key     string
	model   string
	http    *http.Client
}

func New(baseURL, key, model string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, model: model, http: &http.Client{Timeout: 60 * time.Second}}
}

// Question is one typed question. Criteria is a map of label to description
// for "choice", an ordered list of level descriptions for "score", and
// omitted for "noul" (yes/no).
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// SystemOne asks all questions about state in one request. It returns the
// answers and the model that produced them.
func (c *Client) SystemOne(ctx context.Context, state string, questions map[string]Question) (map[string]Answer, string, error) {
	payload, err := json.Marshal(map[string]any{"model": c.model, "state": state, "questions": questions})
	if err != nil {
		return nil, "", err
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			case <-ctx.Done():
				return nil, "", ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/systemone", bytes.NewReader(payload))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("decision model %s: %d %s", c.model, resp.StatusCode, errorMessage(raw))
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				continue
			}
			return nil, "", lastErr
		}
		var res struct {
			Model   string            `json:"model"`
			Answers map[string]Answer `json:"answers"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, "", fmt.Errorf("decision model %s: %w", c.model, err)
		}
		return res.Answers, res.Model, nil
	}
	return nil, "", lastErr
}

func errorMessage(raw []byte) string {
	var e struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if e.Error.Message != "" {
			return e.Error.Message
		}
		if e.Message != "" {
			return e.Message
		}
	}
	return strings.TrimSpace(string(raw))
}
