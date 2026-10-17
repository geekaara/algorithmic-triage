// Command triage decides which backlog tickets an MVP needs and schedules
// them into the sprints left before the deadline.
//
//	triage plan    judge every untriaged ticket, print the plan, write plan.json
//	triage apply   carry out plan.json in Jira
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/geekaara/algorithmic-triage/internal/config"
	"github.com/geekaara/algorithmic-triage/internal/gemini"
	"github.com/geekaara/algorithmic-triage/internal/jira"
	"github.com/geekaara/algorithmic-triage/internal/triage"
	"github.com/geekaara/algorithmic-triage/internal/typesafe"
)

const usage = `usage: triage <plan|apply|reset> [flags]

  plan   -policy context/mvp.md -out plan.json -refresh -judge gemini|jev|combined
  apply  -plan plan.json
  reset  [-yes]   undo apply (shows what it would do unless -yes)`

// Gemini models in order of preference; later ones absorb overload errors.
var models = []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-flash-latest", "gemini-3.5-flash", "gemini-3.1-flash-lite"}

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	config.LoadDotEnv(".env")
	cfg, err := config.Require("JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_PROJECT")
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	jc := jira.New(cfg["JIRA_BASE_URL"], cfg["JIRA_EMAIL"], cfg["JIRA_API_TOKEN"])

	switch os.Args[1] {
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ExitOnError)
		policy := fs.String("policy", "context/mvp.md", "MVP definition the model judges against")
		out := fs.String("out", "", "where to write the plan (default plan.json, or plan-jev.json with -judge jev)")
		refresh := fs.Bool("refresh", false, "ignore cached model answers")
		judgeName := fs.String("judge", "gemini", "who decides MVP/V2/split: gemini, jev (a decision model via Vercel AI Gateway), or combined (jev, escalating unsure tickets to gemini)")
		minConf := fs.Float64("min-confidence", 0.7, "with -judge combined: escalate tickets below this decision-model confidence")
		fs.Parse(os.Args[2:])
		if m := os.Getenv("GEMINI_MODEL"); m != "" {
			models = append([]string{m}, models...)
		}
		var gc *gemini.Client
		if key := os.Getenv("GEMINI_API_KEY"); key != "" {
			gc = gemini.New(key, models...)
		}
		var judge decider
		var label string
		switch *judgeName {
		case "gemini":
			if gc == nil {
				log.Fatal("missing GEMINI_API_KEY (see .env.example)")
			}
			judge, label = &triage.Judge{Gemini: gc, CacheDir: ".triage-cache", Refresh: *refresh}, "Gemini"
			if *out == "" {
				*out = "plan.json"
			}
		case "jev", "combined":
			key := os.Getenv("AI_GATEWAY_API_KEY")
			if key == "" {
				log.Fatal("missing AI_GATEWAY_API_KEY (see .env.example)")
			}
			model := os.Getenv("JEV_MODEL")
			if model == "" {
				model = "typesafe-ai/jev"
			}
			dj := &triage.DecisionJudge{Model: typesafe.New(typesafe.DefaultBaseURL, key, model), CacheDir: ".triage-cache", Refresh: *refresh}
			if gc != nil {
				dj.Text = &triage.Judge{Gemini: gc, CacheDir: ".triage-cache"}
			}
			judge, label = dj, model
			if *out == "" {
				*out = "plan-jev.json"
			}
			if *judgeName == "combined" {
				if gc == nil {
					log.Fatal("-judge combined also needs GEMINI_API_KEY")
				}
				judge = &triage.CombinedJudge{Fast: dj, Slow: dj.Text, MinConfidence: *minConf}
				label = fmt.Sprintf("%s, escalating to Gemini below p=%.2f", model, *minConf)
				if *out == "plan-jev.json" {
					*out = "plan-combined.json"
				}
			}
		default:
			log.Fatalf("unknown -judge %q: use gemini or jev", *judgeName)
		}
		err = runPlan(ctx, jc, judge, label, cfg["JIRA_PROJECT"], *policy, *out)
	case "apply":
		fs := flag.NewFlagSet("apply", flag.ExitOnError)
		in := fs.String("plan", "plan.json", "plan written by `triage plan`")
		fs.Parse(os.Args[2:])
		err = runApply(ctx, jc, cfg["JIRA_PROJECT"], *in)
	case "reset":
		fs := flag.NewFlagSet("reset", flag.ExitOnError)
		yes := fs.Bool("yes", false, "actually undo; without it, only show what would change")
		fs.Parse(os.Args[2:])
		err = runReset(ctx, jc, cfg["JIRA_PROJECT"], *yes)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// decider is anything that can judge every candidate ticket: an LLM
// (Gemini) or a decision model (Jev).
type decider interface {
	DecideAll(ctx context.Context, b *triage.Board, progress func(triage.Decision)) (map[string]triage.Decision, error)
}

func runPlan(ctx context.Context, jc *jira.Client, judge decider, label, project, policyPath, out string) error {
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		return err
	}
	start := time.Now()
	b, err := triage.Load(ctx, jc, project)
	if err != nil {
		return err
	}

	fmt.Printf("\n%s  %s · policy %s\n\n", bold("Algorithmic Triage"), project, policyPath)
	hist := make([]string, len(b.Closed))
	for i, s := range b.Closed {
		hist[i] = fmt.Sprintf("%g", s.Completed)
	}
	fmt.Printf("  %-10s %s  =  %s pts/sprint (last %d closed sprints)\n", dim("Velocity"), strings.Join(hist, " → "), bold(fmt.Sprintf("%g", triage.Velocity(b.Closed, 3))), min(3, len(b.Closed)))
	if len(b.Candidates) == 0 {
		fmt.Println("\n  Nothing to triage: every backlog ticket already has the ai-triaged label.")
		return nil
	}

	fmt.Printf("  %-10s judging %d tickets with %s ", dim("Judge"), len(b.Candidates), label)
	switch j := judge.(type) {
	case *triage.Judge:
		j.Policy = string(policy)
	case *triage.DecisionJudge:
		j.Policy = string(policy)
		if j.Text != nil {
			j.Text.Policy = string(policy)
		}
	case *triage.CombinedJudge:
		j.Fast.Policy = string(policy)
		j.Slow.Policy = string(policy)
	}
	ds, err := judge.DecideAll(ctx, b, func(triage.Decision) { fmt.Print(".") })
	fmt.Println()
	if err != nil {
		return err
	}
	if _, ok := judge.(*triage.CombinedJudge); ok {
		var esc []string
		for _, t := range b.Candidates {
			if ds[t.Key].Escalated {
				esc = append(esc, t.Key)
			}
		}
		fmt.Printf("  %-10s decision model decided %d, escalated %d to Gemini %s\n", dim("Combined"), len(b.Candidates)-len(esc), len(esc), dim(strings.Join(esc, " ")))
	}
	p, err := triage.BuildPlan(b, ds)
	if err != nil {
		return err
	}

	for i, s := range p.Sprints {
		note := ""
		if i == 0 && p.CarryOver > 0 {
			note = dim(fmt.Sprintf("  (%g − %g carried over from %s)", p.Velocity, p.CarryOver, b.Active.Name))
		}
		label := ""
		if i == 0 {
			label = "Capacity"
		}
		fmt.Printf("  %-10s %-26s %3g pts%s\n", dim(label), s.Name, s.Capacity, note)
	}
	fmt.Printf("  %-10s %d tickets · %g pts estimated\n\n", dim("Backlog"), len(b.Candidates), p.BacklogPts)

	printTable(b, p)
	printSummary(p, time.Since(start))

	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("\n  Plan written to %s. Nothing in Jira has changed. Run %s to carry it out.\n\n", out, bold("triage apply"))
	return nil
}

func printTable(b *triage.Board, p triage.Plan) {
	where := map[string]string{}
	pts := map[string]float64{}
	for _, s := range p.Sprints {
		for _, it := range s.Items {
			id := it.Key
			if id == "" {
				id = it.SplitFrom
			}
			where[id] = shortSprint(s.Name)
			pts[id] = it.Points
		}
	}
	for _, it := range p.Overflow {
		id := it.Key
		if id == "" {
			id = it.SplitFrom
		}
		where[id] = "AT RISK"
		pts[id] = it.Points
	}

	fmt.Printf("  %s\n", dim(fmt.Sprintf("%-8s %4s  %-8s  %-6s  %-18s %s", "KEY", "PTS", "PRIORITY", "VERDICT", "PLACED", "WHY")))
	for _, t := range b.Candidates {
		d := p.Decisions[t.Key]
		verdict, placed := d.Verdict, "V2 backlog"
		if w, ok := where[t.Key]; ok {
			placed = w
			if d.Verdict == triage.V2 {
				verdict = "MVP*" // pulled in by Rule 3
			}
			if d.Verdict == triage.Split {
				placed = fmt.Sprintf("%g pts → %s", pts[t.Key], w)
			}
		}
		points := fmt.Sprintf("%g", t.Points)
		if t.Points == 0 {
			points = fmt.Sprintf("~%g", d.Estimate)
		}
		why := d.Reason
		if verdict == "MVP*" {
			why = "Rule 3: blocks MVP work"
		}
		fmt.Printf("  %-8s %4s  %-8s  %s  %-18s %s\n", t.Key, points, t.Priority, colorVerdict(verdict), placed, dim(truncate(why, 70)))
	}
}

func printSummary(p triage.Plan, took time.Duration) {
	var mvpN, v2N int
	var v2Pts float64
	for _, s := range p.Sprints {
		mvpN += len(s.Items)
	}
	for _, d := range p.V2 {
		v2N++
		v2Pts += d.Estimate
	}
	fmt.Printf("\n  %s\n", bold("Result"))
	loads := make([]string, len(p.Sprints))
	for i, s := range p.Sprints {
		loads[i] = fmt.Sprintf("%s %g/%g", shortSprint(s.Name), s.Load, s.Capacity)
	}
	fmt.Printf("  %-10s %d items · %g pts  (%s)\n", dim("MVP"), mvpN, p.MVPPoints(), strings.Join(loads, " · "))
	fmt.Printf("  %-10s %d tickets\n", dim("V2"), v2N)
	fmt.Printf("  %-10s %d\n", dim("Split"), len(p.Splits))
	if p.Fits() {
		spare := 0.0
		for _, s := range p.Sprints {
			spare += s.Capacity - s.Load
		}
		fmt.Printf("  %-10s %s\n", dim("Deadline"), green(fmt.Sprintf("✔ MVP fits before demo day, %g pts spare", spare)))
	} else {
		var over float64
		keys := make([]string, len(p.Overflow))
		for i, it := range p.Overflow {
			over += it.Points
			keys[i] = it.ID()
		}
		fmt.Printf("  %-10s %s\n", dim("Deadline"), red(fmt.Sprintf("✖ AT RISK: %g pts of MVP work don't fit (%s)", over, strings.Join(keys, ", "))))
	}
	fmt.Printf("  %-10s %.1fs\n", dim("Took"), took.Seconds())
}

func runApply(ctx context.Context, jc *jira.Client, project, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w (run `triage plan` first)", err)
	}
	var p triage.Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	board, err := jc.ScrumBoard(ctx, project)
	if err != nil {
		return err
	}

	for _, s := range p.Sprints {
		var keys []string
		for _, it := range s.Items {
			if it.Key == "" {
				key, err := createSlice(ctx, jc, project, board, it)
				if err != nil {
					return err
				}
				d := p.Decisions[it.SplitFrom]
				var v2 strings.Builder
				for _, x := range d.V2Items {
					fmt.Fprintf(&v2, "- %s\n", x)
				}
				msg := fmt.Sprintf("Algorithmic triage: SPLIT (confidence %.2f)\n\nMVP slice: %s, %g pts, planned for %s.\n\nMoved to V2:\n%s\nRule: %s\n\n%s",
					d.Confidence, key, it.Points, s.Name, v2.String(), d.Rule, d.Reason)
				if err := annotate(ctx, jc, it.SplitFrom, msg, "v2"); err != nil {
					return err
				}
				if err := jc.Relate(ctx, it.SplitFrom, key); err != nil {
					return err
				}
				fmt.Printf("  %s split → %s (%g pts) → %s\n", it.SplitFrom, key, it.Points, s.Name)
				keys = append(keys, key)
				continue
			}
			d := p.Decisions[it.Key]
			msg := fmt.Sprintf("Algorithmic triage: MVP → %s (confidence %.2f)\n\nRule: %s\n\n%s", s.Name, d.Confidence, it.Rule, it.Reason)
			if it.PulledBy != "" {
				msg = fmt.Sprintf("Algorithmic triage: MVP → %s\n\nRule 3: this ticket blocks %s, which is MVP work.", s.Name, it.PulledBy)
			}
			if it.Estimated {
				if err := jc.SetEstimate(ctx, board, it.Key, it.Points); err != nil {
					return err
				}
				msg += fmt.Sprintf("\n\nEstimated at %g pts by the model; please review.", it.Points)
			}
			if err := annotate(ctx, jc, it.Key, msg, "mvp"); err != nil {
				return err
			}
			keys = append(keys, it.Key)
			fmt.Printf("  %s MVP → %s\n", it.Key, s.Name)
		}
		if len(keys) > 0 {
			if err := jc.MoveToSprint(ctx, s.ID, keys); err != nil {
				return err
			}
		}
		goal := fmt.Sprintf("MVP: %g of %g pts planned by algorithmic triage", s.Load, s.Capacity)
		if err := jc.UpdateSprint(ctx, s.ID, map[string]any{"goal": goal}); err != nil {
			return err
		}
	}

	for _, d := range p.V2 {
		msg := fmt.Sprintf("Algorithmic triage: V2 (confidence %.2f)\n\nRule: %s\n\n%s", d.Confidence, d.Rule, d.Reason)
		if err := annotate(ctx, jc, d.Key, msg, "v2"); err != nil {
			return err
		}
		fmt.Printf("  %s V2\n", d.Key)
	}
	for _, it := range p.Overflow {
		msg := fmt.Sprintf("Algorithmic triage: MVP, but it does not fit before the deadline. Deadline at risk.\n\nRule: %s\n\n%s", it.Rule, it.Reason)
		if it.Key != "" {
			if err := annotate(ctx, jc, it.Key, msg, "mvp", "deadline-at-risk"); err != nil {
				return err
			}
		}
		fmt.Printf("  %s AT RISK\n", it.ID())
	}
	fmt.Printf("\n  Applied %s to %s.\n", path, project)
	return nil
}

const (
	commentPrefix = "Algorithmic triage"
	goalSuffix    = "planned by algorithmic triage"
)

// runReset undoes what apply did, so a demo or test can be run again. It
// only touches what apply creates: its labels, sprint moves, sprint goals,
// model estimates and its own comments. Tickets created by a split are
// listed but never deleted.
func runReset(ctx context.Context, jc *jira.Client, project string, yes bool) error {
	board, err := jc.ScrumBoard(ctx, project)
	if err != nil {
		return err
	}
	pointsField, err := jc.EstimationField(ctx, board)
	if err != nil {
		return err
	}

	triaged, err := jc.SearchIssues(ctx, fmt.Sprintf("project = %s AND labels = %s ORDER BY key", project, "ai-triaged"), []string{"summary", "description"})
	if err != nil {
		return err
	}
	inSprints, err := jc.SearchIssues(ctx, fmt.Sprintf("project = %s AND labels = %s AND sprint IN futureSprints()", project, "ai-triaged"), []string{"summary"})
	if err != nil {
		return err
	}
	// Comments can outlive labels (for example after a manual undo), so look
	// for ours on every ticket in the project.
	all, err := jc.SearchIssues(ctx, fmt.Sprintf("project = %s AND issuetype != Epic ORDER BY key", project), []string{"summary"})
	if err != nil {
		return err
	}

	type commentRef struct{ key, id string }
	var comments []commentRef
	var unestimate []string
	for _, is := range all {
		cs, err := jc.Comments(ctx, is.Key)
		if err != nil {
			return err
		}
		estimated := false
		for _, c := range cs {
			if strings.HasPrefix(c.Text, commentPrefix) {
				comments = append(comments, commentRef{is.Key, c.ID})
				estimated = estimated || strings.Contains(c.Text, "by the model; please review")
			}
		}
		if estimated {
			unestimate = append(unestimate, is.Key)
		}
	}
	var splits []string
	for _, is := range triaged {
		if strings.Contains(jira.PlainText(is.Fields["description"]), "split by algorithmic triage") {
			splits = append(splits, is.Key)
		}
	}

	sprints, err := jc.Sprints(ctx, board)
	if err != nil {
		return err
	}
	var goals []jira.Sprint
	for _, s := range sprints {
		if s.State == "future" && strings.HasSuffix(s.Goal, goalSuffix) {
			goals = append(goals, s)
		}
	}

	moveKeys := make([]string, len(inSprints))
	for i, is := range inSprints {
		moveKeys[i] = is.Key
	}
	fmt.Printf("\n%s  %s\n\n", bold("Reset"), project)
	fmt.Printf("  %-26s %d %s\n", "Move back to backlog", len(moveKeys), dim(strings.Join(moveKeys, " ")))
	fmt.Printf("  %-26s %d tickets\n", "Remove triage labels", len(triaged))
	fmt.Printf("  %-26s %d\n", "Delete triage comments", len(comments))
	fmt.Printf("  %-26s %d %s\n", "Clear model estimates", len(unestimate), dim(strings.Join(unestimate, " ")))
	fmt.Printf("  %-26s %d\n", "Clear sprint goals", len(goals))
	if len(splits) > 0 {
		fmt.Printf("  %-26s %s\n", "Split tickets (kept)", strings.Join(splits, " ")+dim("  delete by hand if you no longer want them"))
	}
	if !yes {
		fmt.Printf("\n  Nothing changed. Run %s to do it.\n\n", bold("triage reset -yes"))
		return nil
	}

	if err := jc.MoveToBacklog(ctx, moveKeys); err != nil {
		return err
	}
	for _, c := range comments {
		if err := jc.DeleteComment(ctx, c.key, c.id); err != nil {
			return err
		}
	}
	for _, k := range unestimate {
		if err := jc.EditIssue(ctx, k, map[string]any{"fields": map[string]any{pointsField: nil}}); err != nil {
			return err
		}
	}
	for _, is := range triaged {
		if err := jc.RemoveLabels(ctx, is.Key, "ai-triaged", "mvp", "v2", "deadline-at-risk"); err != nil {
			return err
		}
	}
	for _, s := range goals {
		if err := jc.UpdateSprint(ctx, s.ID, map[string]any{"goal": ""}); err != nil {
			return err
		}
	}
	fmt.Printf("\n  %s Run %s to see the plan again.\n\n", green("✔ Reset done."), bold("triage plan"))
	return nil
}

func createSlice(ctx context.Context, jc *jira.Client, project string, board int, it triage.Item) (string, error) {
	var desc strings.Builder
	fmt.Fprintf(&desc, "MVP slice of %s, split by algorithmic triage (Rule 2).\n\n## Acceptance criteria\n", it.SplitFrom)
	for _, c := range it.Criteria {
		fmt.Fprintf(&desc, "- %s\n", c)
	}
	fields := map[string]any{
		"project":     map[string]string{"key": project},
		"issuetype":   map[string]string{"name": "Story"},
		"summary":     it.Summary,
		"description": jira.ADF(desc.String()),
		"priority":    map[string]string{"name": it.Priority},
		"labels":      []string{"mvp", "ai-triaged"},
	}
	if it.EpicKey != "" {
		fields["parent"] = map[string]string{"key": it.EpicKey}
	}
	key, err := jc.CreateIssue(ctx, fields)
	if err != nil {
		return "", err
	}
	return key, jc.SetEstimate(ctx, board, key, it.Points)
}

func annotate(ctx context.Context, jc *jira.Client, key, msg string, labels ...string) error {
	if err := jc.AddLabels(ctx, key, append(labels, "ai-triaged")...); err != nil {
		return err
	}
	return jc.Comment(ctx, key, msg)
}

// ---- terminal output -----------------------------------------------------

var color = func() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
}()

func paint(code, s string) string {
	if !color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string  { return paint("1", s) }
func dim(s string) string   { return paint("2", s) }
func green(s string) string { return paint("32", s) }
func red(s string) string   { return paint("31", s) }

func colorVerdict(v string) string {
	padded := fmt.Sprintf("%-6s", v)
	switch v {
	case triage.MVP, "MVP*":
		return paint("1;32", padded)
	case triage.Split:
		return paint("1;33", padded)
	default:
		return paint("31", padded)
	}
}

func shortSprint(name string) string {
	name = strings.TrimPrefix(name, "TRIP ")
	if i := strings.Index(name, " ("); i > 0 {
		name = name[:i]
	}
	return name
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
