package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Issue is an issue with its raw fields, for callers that need more than
// IssueRef but don't want a fixed struct for every custom field.
type Issue struct {
	Key    string                     `json:"key"`
	Fields map[string]json.RawMessage `json:"fields"`
}

// SearchIssues returns every issue matching jql with the requested fields.
func (c *Client) SearchIssues(ctx context.Context, jql string, fields []string) ([]Issue, error) {
	var out []Issue
	token := ""
	for {
		var res struct {
			Issues        []Issue `json:"issues"`
			NextPageToken string  `json:"nextPageToken"`
		}
		in := map[string]any{"jql": jql, "maxResults": 100, "fields": fields}
		if token != "" {
			in["nextPageToken"] = token
		}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", in, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Issues...)
		if token = res.NextPageToken; token == "" {
			return out, nil
		}
	}
}

// EstimationField returns the field a board uses for estimates.
func (c *Client) EstimationField(ctx context.Context, boardID int) (string, error) {
	var res struct {
		Estimation struct {
			Field struct {
				FieldID string `json:"fieldId"`
			} `json:"field"`
		} `json:"estimation"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/rest/agile/1.0/board/%d/configuration", boardID), nil, &res); err != nil {
		return "", err
	}
	if res.Estimation.Field.FieldID == "" {
		return "", fmt.Errorf("jira: board %d has no estimation field", boardID)
	}
	return res.Estimation.Field.FieldID, nil
}

// AddLabels adds labels to an issue without touching existing ones.
func (c *Client) AddLabels(ctx context.Context, key string, labels ...string) error {
	ops := make([]map[string]string, len(labels))
	for i, l := range labels {
		ops[i] = map[string]string{"add": l}
	}
	in := map[string]any{"update": map[string]any{"labels": ops}}
	return c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), in, nil)
}

// Comment adds a comment written in the Markdown subset ADF understands.
func (c *Client) Comment(ctx context.Context, key, md string) error {
	return c.do(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", map[string]any{"body": ADF(md)}, nil)
}

// RemoveLabels removes labels from an issue, leaving any others in place.
func (c *Client) RemoveLabels(ctx context.Context, key string, labels ...string) error {
	ops := make([]map[string]string, len(labels))
	for i, l := range labels {
		ops[i] = map[string]string{"remove": l}
	}
	in := map[string]any{"update": map[string]any{"labels": ops}}
	return c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), in, nil)
}

type Comment struct {
	ID   string
	Text string
}

// Comments returns an issue's comments as plain text.
func (c *Client) Comments(ctx context.Context, key string) ([]Comment, error) {
	var out []Comment
	for start := 0; ; {
		var res struct {
			Comments []struct {
				ID   string          `json:"id"`
				Body json.RawMessage `json:"body"`
			} `json:"comments"`
			Total int `json:"total"`
		}
		path := fmt.Sprintf("/rest/api/3/issue/%s/comment?startAt=%d", url.PathEscape(key), start)
		if err := c.do(ctx, http.MethodGet, path, nil, &res); err != nil {
			return nil, err
		}
		for _, cm := range res.Comments {
			out = append(out, Comment{ID: cm.ID, Text: PlainText(cm.Body)})
		}
		start += len(res.Comments)
		if len(res.Comments) == 0 || start >= res.Total {
			return out, nil
		}
	}
}

func (c *Client) DeleteComment(ctx context.Context, key, id string) error {
	return c.do(ctx, http.MethodDelete, "/rest/api/3/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(id), nil, nil)
}

// MoveToBacklog takes issues out of their sprints, 50 at a time.
func (c *Client) MoveToBacklog(ctx context.Context, keys []string) error {
	for len(keys) > 0 {
		n := min(len(keys), 50)
		if err := c.do(ctx, http.MethodPost, "/rest/agile/1.0/backlog/issue", map[string]any{"issues": keys[:n]}, nil); err != nil {
			return err
		}
		keys = keys[n:]
	}
	return nil
}

// Relate links two issues with Jira's built-in "Relates" type.
func (c *Client) Relate(ctx context.Context, a, b string) error {
	in := map[string]any{
		"type":         map[string]string{"name": "Relates"},
		"inwardIssue":  map[string]string{"key": a},
		"outwardIssue": map[string]string{"key": b},
	}
	return c.do(ctx, http.MethodPost, "/rest/api/3/issueLink", in, nil)
}

// PlainText flattens an ADF document into text, keeping paragraphs and
// list items on their own lines.
func PlainText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var node adfNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	var sb strings.Builder
	node.write(&sb)
	return strings.TrimSpace(sb.String())
}

type adfNode struct {
	Type    string    `json:"type"`
	Text    string    `json:"text"`
	Content []adfNode `json:"content"`
}

func (n adfNode) write(sb *strings.Builder) {
	switch n.Type {
	case "text":
		sb.WriteString(n.Text)
		return
	case "listItem":
		sb.WriteString("- ")
	case "hardBreak":
		sb.WriteString("\n")
		return
	}
	for _, c := range n.Content {
		c.write(sb)
	}
	switch n.Type {
	case "paragraph", "heading":
		sb.WriteString("\n")
	}
}
