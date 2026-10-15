package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geekaara/algorithmic-triage/internal/gemini"
)

// Decision is the model's judgement on one ticket. It decides *what*;
// planning code decides *how much fits*.
type Decision struct {
	Key            string   `json:"key"`
	Reason         string   `json:"reason"`
	Rule           string   `json:"rule"`
	Verdict        string   `json:"verdict"` // MVP | V2 | SPLIT
	GoldenPathStep int      `json:"golden_path_step"`
	Estimate       float64  `json:"estimate"`
	SplitSummary   string   `json:"split_summary,omitempty"`
	SplitCriteria  []string `json:"split_criteria,omitempty"`
	SplitPoints    float64  `json:"split_points,omitempty"`
	V2Items        []string `json:"v2_items,omitempty"`
	Confidence     float64  `json:"confidence"`
	Model          string   `json:"model,omitempty"`
}

const (
	MVP   = "MVP"
	V2    = "V2"
	Split = "SPLIT"
)

// The response schema. Reason and rule come first so the model commits to
// its justification before its verdict.
var decisionSchema = map[string]any{
	"type": "OBJECT",
	"properties": map[string]any{
		"reason":           map[string]any{"type": "STRING", "description": "One or two plain sentences a team member would accept."},
		"rule":             map[string]any{"type": "STRING", "description": "The MVP definition line or section applied, quoting a few of its words."},
		"verdict":          map[string]any{"type": "STRING", "enum": []string{MVP, V2, Split}},
		"golden_path_step": map[string]any{"type": "INTEGER", "description": "1-5 if the ticket (or its MVP slice) serves that golden-path step, else 0."},
		"estimate":         map[string]any{"type": "NUMBER", "description": "Story points for the MVP part: 1, 2, 3, 5, 8 or 13."},
		"split_summary":    map[string]any{"type": "STRING", "description": "SPLIT only: summary for the new MVP-slice ticket."},
		"split_criteria":   map[string]any{"type": "ARRAY", "items": map[string]any{"type": "STRING"}, "description": "SPLIT only: acceptance criteria for the MVP slice."},
		"split_points":     map[string]any{"type": "NUMBER", "description": "SPLIT only: story points for the MVP slice."},
		"v2_items":         map[string]any{"type": "ARRAY", "items": map[string]any{"type": "STRING"}, "description": "SPLIT only: the parts moving to V2."},
		"confidence":       map[string]any{"type": "NUMBER", "description": "0 to 1."},
	},
	"required":         []string{"reason", "rule", "verdict", "golden_path_step", "estimate", "confidence"},
	"propertyOrdering": []string{"reason", "rule", "verdict", "golden_path_step", "estimate", "split_summary", "split_criteria", "split_points", "v2_items", "confidence"},
}

var batchSchema = func() map[string]any {
	item := map[string]any{}
	for k, v := range decisionSchema {
		item[k] = v
	}
	props := map[string]any{"key": map[string]any{"type": "STRING", "description": "The ticket key, e.g. TRIP-35."}}
	for k, v := range decisionSchema["properties"].(map[string]any) {
		props[k] = v
	}
	item["properties"] = props
	item["required"] = append([]string{"key"}, decisionSchema["required"].([]string)...)
	item["propertyOrdering"] = append([]string{"key"}, decisionSchema["propertyOrdering"].([]string)...)
	return map[string]any{
		"type":       "OBJECT",
		"properties": map[string]any{"decisions": map[string]any{"type": "ARRAY", "items": item}},
		"required":   []string{"decisions"},
	}
}()

const instructions = `You are the delivery lead for the product described in the MVP definition below. You triage one Jira ticket at a time against that definition.

Verdicts:
- MVP: the whole ticket is needed for a golden-path step or a non-negotiable.
- V2: none of it is needed before the deadline.
- SPLIT: part of it is needed and part is not (Rule 2). Describe only the MVP slice as a new ticket and list the parts moving to V2.

How to judge:
- Judge the ticket on its own merits. Dependencies between tickets are resolved by separate code; do not keep a ticket only because other work might depend on it.
- Priority labels and who requested the work are inputs, never the deciding factor (Rule 4).
- A bug that breaks or degrades a golden-path step on mobile web is MVP.
- Quote the specific line or section of the MVP definition you applied in "rule".
- Always give "estimate"; it is only used when the ticket has no estimate.

MVP definition:
---
%s
---`

// Judge asks the model about each ticket, a few at a time. Answers are
// cached on disk by a hash of the full prompt, so re-runs are free,
// instant and identical unless the ticket or the policy changes.
type Judge struct {
	Gemini   *gemini.Client
	Policy   string
	CacheDir string
	Refresh  bool
}

// batchSize keeps each request small enough to judge carefully while
// staying inside per-minute request quotas.
const batchSize = 8

func (j *Judge) DecideAll(ctx context.Context, b *Board, progress func(Decision)) (map[string]Decision, error) {
	system := fmt.Sprintf(instructions, j.Policy)
	out := map[string]Decision{}
	prompts := map[string]string{}
	var pending []Ticket
	for _, t := range b.Candidates {
		prompts[t.Key] = describe(t, b)
		if d, ok := j.cached(system, prompts[t.Key]); ok && !j.Refresh {
			d.Key = t.Key
			out[t.Key] = d
			progress(d)
			continue
		}
		pending = append(pending, t)
	}

	for len(pending) > 0 {
		n := min(batchSize, len(pending))
		batch := pending[:n]
		pending = pending[n:]

		var sb strings.Builder
		fmt.Fprintf(&sb, "Triage each of these %d tickets independently. Return one decision per ticket, with its key.\n", n)
		for i, t := range batch {
			fmt.Fprintf(&sb, "\n=== Ticket %d of %d ===\n%s", i+1, n, prompts[t.Key])
		}
		var res struct {
			Decisions []Decision `json:"decisions"`
		}
		model, err := j.Gemini.GenerateJSON(ctx, system, sb.String(), batchSchema, &res)
		if err != nil {
			return out, err
		}
		got := map[string]Decision{}
		for _, d := range res.Decisions {
			got[d.Key] = d
		}
		for _, t := range batch {
			d, ok := got[t.Key]
			if !ok {
				return out, fmt.Errorf("%s: model returned no decision", t.Key)
			}
			if d.Verdict != MVP && d.Verdict != V2 && d.Verdict != Split {
				return out, fmt.Errorf("%s: unexpected verdict %q", t.Key, d.Verdict)
			}
			d.Model = model
			j.store(system, prompts[t.Key], d)
			out[t.Key] = d
			progress(d)
		}
	}
	return out, nil
}

// Each decision is cached under its own single-ticket prompt, so a ticket
// is only re-judged when that ticket or the policy changes.
func (j *Judge) cachePath(system, user string) string {
	sum := sha256.Sum256([]byte(system + "\x00" + user))
	return filepath.Join(j.CacheDir, hex.EncodeToString(sum[:8])+".json")
}

func (j *Judge) cached(system, user string) (Decision, bool) {
	var d Decision
	raw, err := os.ReadFile(j.cachePath(system, user))
	return d, err == nil && json.Unmarshal(raw, &d) == nil
}

func (j *Judge) store(system, user string, d Decision) {
	if err := os.MkdirAll(j.CacheDir, 0o755); err != nil {
		return
	}
	raw, _ := json.MarshalIndent(d, "", "  ")
	_ = os.WriteFile(j.cachePath(system, user), raw, 0o644)
}

func describe(t Ticket, b *Board) string {
	var sb strings.Builder
	pts := "unestimated"
	if t.Points > 0 {
		pts = fmt.Sprintf("%g story points", t.Points)
	}
	fmt.Fprintf(&sb, "Ticket %s (%s, priority %s, %s)\n", t.Key, t.Type, t.Priority, pts)
	if t.Epic != "" {
		fmt.Fprintf(&sb, "Epic: %s\n", t.Epic)
	}
	fmt.Fprintf(&sb, "Summary: %s\n\nDescription:\n%s\n", t.Summary, t.Description)
	related := func(label string, keys []string) {
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s %s: %s\n", label, k, b.All[k].Summary)
		}
	}
	if len(t.Blocks)+len(t.BlockedBy) > 0 {
		sb.WriteString("\nDependencies:\n")
		related("Blocks", t.Blocks)
		related("Blocked by", t.BlockedBy)
	}
	return sb.String()
}
