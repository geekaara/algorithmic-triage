// Package gemini calls the Gemini API's generateContent endpoint with a
// response schema, so every answer is JSON that matches a known shape.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent"

type Client struct {
	key    string
	models []string // tried in order; later ones are fallbacks for overload
	http   *http.Client
}

func New(key string, models ...string) *Client {
	return &Client{key: key, models: models, http: &http.Client{Timeout: 90 * time.Second}}
}

// GenerateJSON sends system and user text, constrains the reply to schema,
// and decodes it into out. It returns the model version that answered.
func (c *Client) GenerateJSON(ctx context.Context, system, user string, schema map[string]any, out any) (string, error) {
	body := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]string{"text": system}}},
		"contents":          []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": user}}}},
		"generationConfig": map[string]any{
			"temperature":      0,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	var lastErr error
	for _, model := range c.models {
		for attempt := 0; attempt < 6; attempt++ {
			version, text, status, err := c.call(ctx, model, payload)
			if err == nil {
				if err := json.Unmarshal([]byte(text), out); err != nil {
					lastErr = fmt.Errorf("gemini %s: invalid JSON: %w", model, err)
					continue
				}
				return version, nil
			}
			lastErr = err
			if status != http.StatusTooManyRequests && status < 500 {
				return "", err // not transient
			}
			if status >= 500 && attempt >= 2 {
				break // overloaded; move on to the next model
			}
			wait := time.Duration(attempt+1) * 2 * time.Second
			if status == http.StatusTooManyRequests {
				wait = retryDelay(err.Error()) // the error says how long to wait
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "", lastErr
}

var retryIn = regexp.MustCompile(`retry in ([0-9.]+)s`)

func retryDelay(msg string) time.Duration {
	if m := retryIn.FindStringSubmatch(msg); m != nil {
		if s, err := strconv.ParseFloat(m[1], 64); err == nil {
			return time.Duration(s*float64(time.Second)) + time.Second
		}
	}
	return 30 * time.Second
}

func (c *Client) call(ctx context.Context, model string, payload []byte) (version, text string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(endpoint, model), bytes.NewReader(payload))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", resp.StatusCode, err
	}

	var res struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		ModelVersion string `json:"modelVersion"`
		Error        *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", "", resp.StatusCode, fmt.Errorf("gemini %s: %d %s", model, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if res.Error != nil {
		return "", "", resp.StatusCode, fmt.Errorf("gemini %s: %d %s", model, resp.StatusCode, res.Error.Message)
	}
	if len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return "", "", resp.StatusCode, fmt.Errorf("gemini %s: empty response", model)
	}
	var sb strings.Builder
	for _, p := range res.Candidates[0].Content.Parts {
		sb.WriteString(p.Text)
	}
	return res.ModelVersion, sb.String(), resp.StatusCode, nil
}
