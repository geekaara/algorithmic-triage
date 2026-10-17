package triage

import (
	"context"
	"fmt"
)

// CombinedJudge gives each model the job it is best at.
//
// The decision model (Jev) classifies every ticket: verdict, the policy line
// that decides it, golden-path step and estimate, in seconds, with
// calibrated confidence, choosing only from options that exist.
//
// The LLM (Gemini) does what needs judgement or language. It writes the
// explanation for each confident decision, and objects if one clearly
// contradicts the policy. It then makes the final call on every ticket the
// decision model was unsure about, wanted to split, or was objected to,
// writing a split ticket's text where needed.
type CombinedJudge struct {
	Fast          *DecisionJudge
	Slow          *Judge
	MinConfidence float64
}

func (j *CombinedJudge) DecideAll(ctx context.Context, b *Board, progress func(Decision)) (map[string]Decision, error) {
	fast := *j.Fast
	fast.Text = nil // splits are escalated below instead
	ds, err := fast.DecideAll(ctx, b, progress)
	if err != nil || j.Slow == nil {
		return ds, err
	}

	var escalate []Ticket
	why := map[string]string{}
	confident := map[string]Decision{}
	for _, t := range b.Candidates {
		d := ds[t.Key]
		switch {
		case d.Verdict == Split:
			escalate, why[t.Key] = append(escalate, t), "decision model suggested a split"
		case d.Confidence < j.MinConfidence:
			escalate, why[t.Key] = append(escalate, t), fmt.Sprintf("decision model unsure, p=%.2f", d.Confidence)
		default:
			confident[t.Key] = d
		}
	}

	// 1. The LLM explains each confident decision, or objects to it. If the
	// explanations fail, keep the decisions with their template reasons.
	if len(confident) > 0 {
		ex, err := j.Slow.Explain(ctx, b, confident)
		if err != nil {
			fmt.Printf("\n  note: could not write all explanations (%v); kept template reasons\n", err)
		}
		for _, t := range b.Candidates {
			d, ok := confident[t.Key]
			e, explained := ex[t.Key]
			if !ok || !explained {
				continue
			}
			if e.Objection {
				escalate = append(escalate, t)
				why[t.Key] = fmt.Sprintf("LLM objected to the decision model's %s (p=%.2f)", d.Verdict, d.Confidence)
				continue
			}
			d.Reason = fmt.Sprintf("%s (decision model, p=%.2f)", e.Reason, d.Confidence)
			ds[t.Key] = d
		}
	}

	// 2. The LLM makes the final call where the decision model was unsure,
	// wanted a split, or was objected to.
	if len(escalate) > 0 {
		slow, err := j.Slow.DecideAll(ctx, &Board{Candidates: escalate, All: b.All}, func(Decision) {})
		if err != nil {
			return ds, fmt.Errorf("escalating to the LLM: %w", err)
		}
		for _, t := range escalate {
			sd := slow[t.Key]
			sd.Key = t.Key
			sd.Reason = fmt.Sprintf("%s (decided by the LLM: %s)", sd.Reason, why[t.Key])
			sd.Escalated = true
			ds[t.Key] = sd
		}
	}
	return ds, nil
}
