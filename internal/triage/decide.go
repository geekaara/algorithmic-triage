package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/geekaara/algorithmic-triage/internal/typesafe"
)

// DecisionJudge judges tickets with a System One decision model such as
// Jev. The model only picks from fixed options, so every verdict, rule and
// golden-path step it returns is one that exists in mvp.md, and every answer
// comes with calibrated probabilities. It cannot write text, so reasons are
// built from the rule it chose, and the wording of a SPLIT ticket's MVP
// slice comes from the text judge (Gemini) when one is configured.
type DecisionJudge struct {
	Model    *typesafe.Client
	Policy   string
	CacheDir string
	Refresh  bool
	Text     *Judge // optional: writes split-ticket text
}

// Fibonacci points for the estimate scale's levels 0..5.
var pointLevels = []float64{1, 2, 3, 5, 8, 13}

func (j *DecisionJudge) questions(p Policy) map[string]typesafe.Question {
	// Only concrete policy lines are citable. Meta-rules like "golden-path
	// test" describe how to decide, not why, and a classifier will happily
	// pick them for everything.
	rules := map[string]string{}
	for _, l := range p.Order {
		if !strings.HasPrefix(l, "rule-") {
			rules[l] = p.Describe(l)
		}
	}
	steps := map[string]string{"0": "Serves none of the golden-path steps."}
	for i, s := range p.Steps {
		steps[strconv.Itoa(i+1)] = s
	}
	return map[string]typesafe.Question{
		"verdict": {
			Type:         "choice",
			Instructions: "Triage the ticket against the MVP policy. Priority labels and who asked for it are inputs, never the deciding factor.",
			Criteria: map[string]string{
				MVP:   "The whole ticket is needed for a golden-path step or a non-negotiable.",
				V2:    "None of the ticket is needed before the deadline.",
				Split: "Part of the ticket is needed for the MVP and part of it is not.",
			},
		},
		"rule": {
			Type:         "choice",
			Instructions: "Which single, specific line of the MVP policy best explains this ticket's verdict: the golden-path step it serves, the non-negotiable it meets, or the out-of-scope item it falls under?",
			Criteria:     rules,
		},
		"step": {
			Type:         "choice",
			Instructions: "Which golden-path step does the ticket, or its MVP part, serve?",
			Criteria:     steps,
		},
		"estimate": {
			Type:         "score",
			Instructions: "How much effort is the MVP part of this ticket?",
			Criteria: []string{
				"1 point: a small, contained change of a few hours",
				"2 points: about a day, one component",
				"3 points: two or three days, a couple of components",
				"5 points: about a week, several components",
				"8 points: more than a week, a new integration or service",
				"13 points: weeks of work or new infrastructure",
			},
		},
	}
}

func (j *DecisionJudge) DecideAll(ctx context.Context, b *Board, progress func(Decision)) (map[string]Decision, error) {
	policy := ParsePolicy(j.Policy)
	qs := j.questions(policy)
	qsJSON, _ := json.Marshal(qs)

	out := map[string]Decision{}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		errs  []error
		sem   = make(chan struct{}, 4)
		split []Ticket
	)
	for _, t := range b.Candidates {
		wg.Add(1)
		go func(t Ticket) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			state := "MVP POLICY:\n" + j.Policy + "\n\nTICKET:\n" + describe(t, b)
			cacheKey := "decision\x00" + string(qsJSON)
			var d Decision
			cached := false
			if !j.Refresh {
				d, cached = (&Judge{CacheDir: j.CacheDir}).cached(cacheKey, state)
			}
			if !cached {
				answers, model, err := j.Model.SystemOne(ctx, state, qs)
				if err != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("%s: %w", t.Key, err))
					mu.Unlock()
					return
				}
				d = fromAnswers(answers, policy)
				d.Model = model
				(&Judge{CacheDir: j.CacheDir}).store(cacheKey, state, d)
			}
			d.Key = t.Key
			mu.Lock()
			out[t.Key] = d
			if d.Verdict == Split {
				split = append(split, t)
			}
			progress(d)
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	if len(errs) > 0 {
		return out, errs[0]
	}

	// The decision model chose SPLIT; it can't write the new ticket, so ask
	// the text judge for the slice's wording, or fall back to a placeholder
	// the team refines.
	if len(split) > 0 {
		var texts map[string]Decision
		if j.Text != nil {
			sub := &Board{Candidates: split, All: b.All}
			texts, _ = j.Text.DecideAll(ctx, sub, func(Decision) {})
		}
		for _, t := range split {
			d := out[t.Key]
			if td, ok := texts[t.Key]; ok && td.SplitSummary != "" {
				d.SplitSummary, d.SplitCriteria, d.V2Items = td.SplitSummary, td.SplitCriteria, td.V2Items
			} else {
				d.SplitSummary = "MVP slice of: " + t.Summary
			}
			d.SplitPoints = d.Estimate
			out[t.Key] = d
		}
	}
	return out, nil
}

func fromAnswers(a map[string]typesafe.Answer, p Policy) Decision {
	d := Decision{
		Verdict:    a["verdict"].Choice,
		Confidence: a["verdict"].Confidence,
		Rule:       p.Describe(a["rule"].Choice),
	}
	d.GoldenPathStep, _ = strconv.Atoi(a["step"].Choice)
	level := int(math.Round(a["estimate"].Score))
	level = max(0, min(level, len(pointLevels)-1))
	d.Estimate = pointLevels[level]

	pv := a["verdict"].Probabilities[d.Verdict]
	switch d.Verdict {
	case MVP:
		d.Reason = fmt.Sprintf("Needed for the MVP. %s (p=%.2f)", d.Rule, pv)
	case V2:
		d.Reason = fmt.Sprintf("Not needed before the deadline. %s (p=%.2f)", d.Rule, pv)
	default:
		d.Reason = fmt.Sprintf("Part MVP, part V2. %s (p=%.2f)", d.Rule, pv)
	}
	return d
}
