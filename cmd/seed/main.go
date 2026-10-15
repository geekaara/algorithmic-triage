// Command seed populates a Jira Scrum project with the Tripline demo
// backlog: epics, stories with estimates, dependencies and sprint history.
//
//	go run ./cmd/seed -dry-run   # print the plan, touch nothing
//	go run ./cmd/seed            # create everything in JIRA_PROJECT
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/geekaara/algorithmic-triage/internal/config"
	"github.com/geekaara/algorithmic-triage/internal/jira"
)

type sprintSpec struct {
	Ref, Name, Goal, Start, End string
	State                       string // closed | active | future
}

type issueSpec struct {
	Ref, Type, Epic, Sprint, Status string
	Summary, Description            string
	Priority                        string
	Labels                          []string
	Points                          float64 // 0 means unestimated
}

// Sprint boundaries in Brisbane time.
const tz = "+10:00"

func main() {
	dryRun := flag.Bool("dry-run", false, "print the plan without calling Jira")
	flag.Parse()

	if err := validate(); err != nil {
		log.Fatal(err)
	}
	printPlan()
	if *dryRun {
		return
	}

	config.LoadDotEnv(".env")
	cfg, err := config.Require("JIRA_BASE_URL", "JIRA_EMAIL", "JIRA_API_TOKEN", "JIRA_PROJECT")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	c := jira.New(cfg["JIRA_BASE_URL"], cfg["JIRA_EMAIL"], cfg["JIRA_API_TOKEN"])
	if err := seed(ctx, c, cfg["JIRA_PROJECT"]); err != nil {
		log.Fatal(err)
	}
}

func seed(ctx context.Context, c *jira.Client, project string) error {
	me, err := c.Myself(ctx)
	if err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	log.Printf("authenticated as %s", me.DisplayName)

	board, err := c.ScrumBoard(ctx, project)
	if err != nil {
		return err
	}

	// Seeding is resumable: issues are matched by summary, so a re-run after
	// a failure picks up where it left off instead of duplicating work.
	found, err := c.Search(ctx, "project = "+project)
	if err != nil {
		return err
	}
	existing := map[string]string{}
	for _, is := range found {
		existing[is.Summary] = is.Key
	}

	keys := map[string]string{} // ref -> issue key
	create := func(s issueSpec, issueType string) error {
		if key, ok := existing[s.Summary]; ok {
			keys[s.Ref] = key
			return nil
		}
		fields := map[string]any{
			"project":     map[string]string{"key": project},
			"issuetype":   map[string]string{"name": issueType},
			"summary":     s.Summary,
			"description": jira.ADF(s.Description),
		}
		if len(s.Labels) > 0 {
			fields["labels"] = s.Labels
		}
		if s.Priority != "" {
			fields["priority"] = map[string]string{"name": s.Priority}
		}
		if s.Epic != "" {
			fields["parent"] = map[string]string{"key": keys[s.Epic]}
		}
		key, err := c.CreateIssue(ctx, fields)
		if err != nil {
			return fmt.Errorf("creating %s: %w", s.Ref, err)
		}
		keys[s.Ref] = key
		log.Printf("%-8s %s", key, s.Summary)
		return nil
	}

	for _, e := range epics {
		if err := create(e, "Epic"); err != nil {
			return err
		}
	}
	for _, is := range issues {
		if err := create(is, is.Type); err != nil {
			return err
		}
	}
	// Estimates go in after creation so the board has had time to index
	// the new issues.
	for _, is := range issues {
		if is.Points > 0 {
			if err := c.SetEstimate(ctx, board, keys[is.Ref], is.Points); err != nil {
				return fmt.Errorf("estimating %s: %w", keys[is.Ref], err)
			}
		}
	}
	log.Printf("estimates set")

	for _, b := range blocks {
		if err := c.Block(ctx, keys[b[0]], keys[b[1]]); err != nil {
			return fmt.Errorf("linking %s blocks %s: %w", b[0], b[1], err)
		}
	}

	return runSprints(ctx, c, project, board, keys)
}

// runSprints replays the sprint history in order: each closed sprint is
// started, its issues completed, then closed, so the board's velocity
// report reflects it. Jira allows only one active sprint at a time.
func runSprints(ctx context.Context, c *jira.Client, project string, board int, keys map[string]string) error {
	existing, err := c.Sprints(ctx, board)
	if err != nil {
		return err
	}
	byName := map[string]jira.Sprint{}
	for _, s := range existing {
		byName[s.Name] = s
	}
	// New company-managed boards come with an empty "<KEY> Sprint 1".
	// Reuse sprints by name so re-runs don't pile up duplicates.

	for _, spec := range sprints {
		// Sprint names follow Jira's "<KEY> Sprint N" convention.
		spec.Name = strings.Replace(spec.Name, "TRIP", project, 1)
		want := jira.Sprint{
			Name:      spec.Name,
			Goal:      spec.Goal,
			StartDate: spec.Start + "T09:00:00.000" + tz,
			EndDate:   spec.End + "T17:00:00.000" + tz,
		}
		sp, ok := byName[spec.Name]
		if ok && sp.State == "closed" {
			continue // already replayed on a previous run
		}
		if !ok {
			if sp, err = c.CreateSprint(ctx, board, want); err != nil {
				return fmt.Errorf("creating %s: %w", spec.Name, err)
			}
		} else if sp.State == "future" {
			if err := c.UpdateSprint(ctx, sp.ID, map[string]any{"goal": want.Goal, "startDate": want.StartDate, "endDate": want.EndDate}); err != nil {
				return err
			}
		}

		var members []issueSpec
		var memberKeys []string
		for _, is := range issues {
			if is.Sprint == spec.Ref {
				members = append(members, is)
				memberKeys = append(memberKeys, keys[is.Ref])
			}
		}
		if len(memberKeys) > 0 {
			if err := c.MoveToSprint(ctx, sp.ID, memberKeys); err != nil {
				return fmt.Errorf("planning %s: %w", spec.Name, err)
			}
		}

		if spec.State == "future" || sp.State == spec.State {
			log.Printf("sprint %q: %d issues (%s)", spec.Name, len(members), spec.State)
			continue
		}

		if sp.State == "future" {
			if err := c.UpdateSprint(ctx, sp.ID, map[string]any{"state": "active", "startDate": want.StartDate, "endDate": want.EndDate}); err != nil {
				return fmt.Errorf("starting %s: %w", spec.Name, err)
			}
		}
		for _, is := range members {
			if cat := category(is.Status); cat != jira.CategoryToDo {
				if err := c.TransitionTo(ctx, keys[is.Ref], cat); err != nil {
					return err
				}
			}
		}
		if spec.State == "closed" {
			if err := c.UpdateSprint(ctx, sp.ID, map[string]any{"state": "closed"}); err != nil {
				return fmt.Errorf("closing %s: %w", spec.Name, err)
			}
		}
		log.Printf("sprint %q: %d issues (%s)", spec.Name, len(members), spec.State)
	}
	return nil
}

func category(status string) string {
	switch status {
	case "Done":
		return jira.CategoryDone
	case "In Progress":
		return jira.CategoryInProgress
	default:
		return jira.CategoryToDo
	}
}

func validate() error {
	refs := map[string]bool{}
	for _, e := range epics {
		refs[e.Ref] = true
	}
	sprintRefs := map[string]string{}
	for _, s := range sprints {
		sprintRefs[s.Ref] = s.State
	}
	var errs []error
	for _, is := range issues {
		if refs[is.Ref] {
			errs = append(errs, fmt.Errorf("duplicate ref %s", is.Ref))
		}
		refs[is.Ref] = true
		if is.Epic != "" && !isEpic(is.Epic) {
			errs = append(errs, fmt.Errorf("%s: unknown epic %s", is.Ref, is.Epic))
		}
		if st, ok := sprintRefs[is.Sprint]; is.Sprint != "" && !ok {
			errs = append(errs, fmt.Errorf("%s: unknown sprint %s", is.Ref, is.Sprint))
		} else if st == "closed" && is.Status != "Done" {
			errs = append(errs, fmt.Errorf("%s: closed-sprint issues must be Done", is.Ref))
		}
	}
	for _, b := range blocks {
		for _, r := range b {
			if !refs[r] {
				errs = append(errs, fmt.Errorf("blocks: unknown ref %s", r))
			}
		}
	}
	return errors.Join(errs...)
}

func isEpic(ref string) bool {
	for _, e := range epics {
		if e.Ref == ref {
			return true
		}
	}
	return false
}

func printPlan() {
	pts := map[string]float64{}
	n := map[string]int{}
	var unestimated int
	for _, is := range issues {
		pts[is.Sprint] += is.Points
		n[is.Sprint]++
		if is.Points == 0 {
			unestimated++
		}
	}
	fmt.Printf("%d epics, %d issues (%d unestimated), %d dependency links\n", len(epics), len(issues), unestimated, len(blocks))
	for _, s := range sprints {
		fmt.Printf("  %-34s %-7s %2d issues  %4.0f pts\n", s.Name, s.State, n[s.Ref], pts[s.Ref])
	}
	fmt.Printf("  %-34s %-7s %2d issues  %4.0f pts\n", "Backlog", "", n[""], pts[""])
}
