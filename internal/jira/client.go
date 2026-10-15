// Package jira is a small client for the parts of the Jira Cloud REST and
// Agile APIs that the triage engine needs.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	email   string
	token   string
	http    *http.Client
}

func New(baseURL, email, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		email:   email,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jira: %s %s: %d %s", e.Method, e.Path, e.Status, e.Body)
}

const maxRetries = 5

// do sends a JSON request and decodes a JSON response into out (if non-nil).
// It retries on 429 and 503, honouring Retry-After.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.email, c.token)
		req.Header.Set("Accept", "application/json")
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && attempt < maxRetries {
			wait := time.Duration(1<<attempt) * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if resp.StatusCode >= 300 {
			return &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
		}
		if out != nil && len(body) > 0 {
			return json.Unmarshal(body, out)
		}
		return nil
	}
}

// ---- Platform API ---------------------------------------------------------

type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

func (c *Client) Myself(ctx context.Context) (User, error) {
	var u User
	err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, &u)
	return u, err
}

// IssueRef is the minimal view of an issue returned by Search.
type IssueRef struct {
	Key     string
	Summary string
}

// Search returns every issue matching jql, following pagination.
func (c *Client) Search(ctx context.Context, jql string) ([]IssueRef, error) {
	var out []IssueRef
	token := ""
	for {
		var res struct {
			Issues []struct {
				Key    string `json:"key"`
				Fields struct {
					Summary string `json:"summary"`
				} `json:"fields"`
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
		}
		in := map[string]any{"jql": jql, "maxResults": 100, "fields": []string{"summary"}}
		if token != "" {
			in["nextPageToken"] = token
		}
		if err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", in, &res); err != nil {
			return nil, err
		}
		for _, is := range res.Issues {
			out = append(out, IssueRef{Key: is.Key, Summary: is.Fields.Summary})
		}
		if token = res.NextPageToken; token == "" {
			return out, nil
		}
	}
}

// CreateIssue creates an issue from a raw fields map and returns its key.
func (c *Client) CreateIssue(ctx context.Context, fields map[string]any) (string, error) {
	var res struct {
		Key string `json:"key"`
	}
	err := c.do(ctx, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": fields}, &res)
	return res.Key, err
}

// Status categories, as reported by Jira.
const (
	CategoryToDo       = "new"
	CategoryInProgress = "indeterminate"
	CategoryDone       = "done"
)

// TransitionTo moves an issue to the first available status in the given
// status category. Workflows differ between projects, so we match on
// category rather than on status names or transition IDs.
func (c *Client) TransitionTo(ctx context.Context, key, category string) error {
	var res struct {
		Transitions []struct {
			ID string `json:"id"`
			To struct {
				StatusCategory struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
		} `json:"transitions"`
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions"
	if err := c.do(ctx, http.MethodGet, path, nil, &res); err != nil {
		return err
	}
	for _, t := range res.Transitions {
		if t.To.StatusCategory.Key == category {
			return c.do(ctx, http.MethodPost, path, map[string]any{"transition": map[string]string{"id": t.ID}}, nil)
		}
	}
	return fmt.Errorf("jira: %s has no transition to a %q status", key, category)
}

// EditIssue applies a raw edit (fields and/or update operations).
func (c *Client) EditIssue(ctx context.Context, key string, body map[string]any) error {
	return c.do(ctx, http.MethodPut, "/rest/api/3/issue/"+url.PathEscape(key), body, nil)
}

// Block records that blocker blocks blocked. Jira's naming is inverted here:
// the inwardIssue of a "Blocks" link is the one that blocks.
func (c *Client) Block(ctx context.Context, blocker, blocked string) error {
	in := map[string]any{
		"type":         map[string]string{"name": "Blocks"},
		"inwardIssue":  map[string]string{"key": blocker},
		"outwardIssue": map[string]string{"key": blocked},
	}
	return c.do(ctx, http.MethodPost, "/rest/api/3/issueLink", in, nil)
}

// ---- Agile API ------------------------------------------------------------

// ScrumBoard returns the ID of the first Scrum board for a project.
func (c *Client) ScrumBoard(ctx context.Context, projectKey string) (int, error) {
	var res struct {
		Values []struct {
			ID int `json:"id"`
		} `json:"values"`
	}
	path := "/rest/agile/1.0/board?type=scrum&projectKeyOrId=" + url.QueryEscape(projectKey)
	if err := c.do(ctx, http.MethodGet, path, nil, &res); err != nil {
		return 0, err
	}
	if len(res.Values) == 0 {
		return 0, fmt.Errorf("jira: no scrum board for project %s", projectKey)
	}
	return res.Values[0].ID, nil
}

// SetEstimate sets the board's estimation field (usually story points),
// which works even when that field is not on the issue's edit screen.
// A freshly created issue can take a few seconds to appear on its board,
// so a 404 is retried briefly.
func (c *Client) SetEstimate(ctx context.Context, boardID int, key string, value float64) error {
	path := fmt.Sprintf("/rest/agile/1.0/issue/%s/estimation?boardId=%d", url.PathEscape(key), boardID)
	in := map[string]string{"value": strconv.FormatFloat(value, 'f', -1, 64)}
	for attempt := 0; ; attempt++ {
		err := c.do(ctx, http.MethodPut, path, in, nil)
		var apiErr *APIError
		if attempt >= 8 || !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			return err
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type Sprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Goal      string `json:"goal,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
}

func (c *Client) Sprints(ctx context.Context, boardID int) ([]Sprint, error) {
	var all []Sprint
	for start := 0; ; {
		var res struct {
			Values []Sprint `json:"values"`
			IsLast bool     `json:"isLast"`
		}
		path := fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?startAt=%d", boardID, start)
		if err := c.do(ctx, http.MethodGet, path, nil, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Values...)
		if res.IsLast || len(res.Values) == 0 {
			return all, nil
		}
		start += len(res.Values)
	}
}

func (c *Client) CreateSprint(ctx context.Context, boardID int, s Sprint) (Sprint, error) {
	in := map[string]any{"name": s.Name, "originBoardId": boardID, "goal": s.Goal}
	if s.StartDate != "" {
		in["startDate"] = s.StartDate
	}
	if s.EndDate != "" {
		in["endDate"] = s.EndDate
	}
	var out Sprint
	err := c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint", in, &out)
	return out, err
}

// UpdateSprint partially updates a sprint. Setting "state" to "active" starts
// it and "closed" completes it.
func (c *Client) UpdateSprint(ctx context.Context, id int, fields map[string]any) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/rest/agile/1.0/sprint/%d", id), fields, nil)
}

// MoveToSprint moves issues into a sprint, 50 at a time (the API limit).
func (c *Client) MoveToSprint(ctx context.Context, sprintID int, keys []string) error {
	for len(keys) > 0 {
		n := min(len(keys), 50)
		path := fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", sprintID)
		if err := c.do(ctx, http.MethodPost, path, map[string]any{"issues": keys[:n]}, nil); err != nil {
			return err
		}
		keys = keys[n:]
	}
	return nil
}
