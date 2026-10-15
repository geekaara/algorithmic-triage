package triage

import (
	"fmt"
	"math"
	"sort"
)

// Item is one unit of MVP work to schedule: an existing ticket, or the MVP
// slice of a ticket that was split (Key empty until it is created).
type Item struct {
	Key       string   `json:"key,omitempty"`
	SplitFrom string   `json:"split_from,omitempty"`
	Summary   string   `json:"summary"`
	Criteria  []string `json:"criteria,omitempty"`
	EpicKey   string   `json:"epic_key,omitempty"`
	Points    float64  `json:"points"`
	Estimated bool     `json:"estimated"` // points came from the model
	Step      int      `json:"golden_path_step"`
	Priority  string   `json:"priority"`
	Rule      string   `json:"rule"`
	Reason    string   `json:"reason"`
	PulledBy  string   `json:"pulled_by,omitempty"` // promoted by Rule 3 for this ticket
	BlockedBy []string `json:"-"`
	rank      int
}

func (it Item) ID() string {
	if it.Key != "" {
		return it.Key
	}
	return "new:" + it.SplitFrom
}

type SprintPlan struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Capacity float64 `json:"capacity"`
	Load     float64 `json:"load"`
	Items    []Item  `json:"items"`
}

type Plan struct {
	Velocity   float64             `json:"velocity"`
	History    []float64           `json:"history"`
	CarryOver  float64             `json:"carry_over"`
	Sprints    []SprintPlan        `json:"sprints"`
	Overflow   []Item              `json:"overflow"`
	V2         []Decision          `json:"v2"`
	Splits     []Decision          `json:"splits"`
	Decisions  map[string]Decision `json:"decisions"`
	BacklogPts float64             `json:"backlog_points"`
}

func (p Plan) Fits() bool { return len(p.Overflow) == 0 }

func (p Plan) MVPPoints() float64 {
	var n float64
	for _, s := range p.Sprints {
		n += s.Load
	}
	return n
}

// Velocity is the mean of the last n closed sprints' completed points.
func Velocity(closed []SprintStat, n int) float64 {
	if len(closed) == 0 {
		return 0
	}
	if len(closed) > n {
		closed = closed[len(closed)-n:]
	}
	var sum float64
	for _, s := range closed {
		sum += s.Completed
	}
	return math.Round(sum/float64(len(closed))*10) / 10
}

// BuildPlan turns per-ticket decisions into a schedule. Everything here is
// deterministic: the same board and decisions always give the same plan.
func BuildPlan(b *Board, ds map[string]Decision) (Plan, error) {
	p := Plan{Decisions: ds}
	for _, s := range b.Closed {
		p.History = append(p.History, s.Completed)
	}
	p.Velocity = Velocity(b.Closed, 3)
	if b.Active != nil {
		// Unfinished work in the current sprint rolls into the next one.
		p.CarryOver = b.Active.Remaining
	}
	if len(b.Future) == 0 {
		return p, fmt.Errorf("no future sprints to plan into")
	}
	for i, s := range b.Future {
		c := p.Velocity
		if i == 0 {
			c = math.Max(0, c-p.CarryOver)
		}
		p.Sprints = append(p.Sprints, SprintPlan{ID: s.ID, Name: s.Name, Capacity: c})
	}

	cand := map[string]Ticket{}
	mvp := map[string]Item{}
	for _, t := range b.Candidates {
		cand[t.Key] = t
		p.BacklogPts += t.Points
		d, ok := ds[t.Key]
		if !ok {
			return p, fmt.Errorf("no decision for %s", t.Key)
		}
		d.Key = t.Key
		switch d.Verdict {
		case MVP:
			it := itemFor(t, d)
			mvp[it.ID()] = it
		case Split:
			p.Splits = append(p.Splits, d)
			it := Item{
				SplitFrom: t.Key, Summary: d.SplitSummary, Criteria: d.SplitCriteria, EpicKey: t.EpicKey,
				Points: d.SplitPoints, Estimated: true, Step: d.GoldenPathStep, Priority: t.Priority,
				Rule: d.Rule, Reason: d.Reason, rank: t.Rank,
				// The slice is new scope: it does not inherit the parent's
				// dependencies, which belonged to the parts moving to V2.
			}
			if it.Points <= 0 {
				it.Points = d.Estimate
			}
			mvp[it.ID()] = it
		case V2:
			p.V2 = append(p.V2, d)
		}
	}

	// Rule 3: anything blocking MVP work is MVP, transitively.
	queue := make([]string, 0, len(mvp))
	for id := range mvp {
		queue = append(queue, id)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, blocker := range mvp[id].BlockedBy {
			t, isCandidate := cand[blocker]
			if _, already := mvp[blocker]; already || !isCandidate {
				continue
			}
			d := ds[blocker]
			it := itemFor(t, d)
			it.PulledBy = mvp[id].ID()
			it.Rule = "Rule 3: a ticket that blocks an MVP ticket is MVP"
			mvp[blocker] = it
			p.V2 = removeDecision(p.V2, blocker)
			queue = append(queue, blocker)
		}
	}

	order := schedule(mvp)
	placed := map[string]int{} // id -> sprint index
	for _, it := range order {
		earliest := 0
		for _, bk := range it.BlockedBy {
			if si, ok := placed[bk]; ok && si > earliest {
				earliest = si
			}
		}
		done := false
		for si := earliest; si < len(p.Sprints); si++ {
			s := &p.Sprints[si]
			if s.Load+it.Points <= s.Capacity {
				s.Items = append(s.Items, it)
				s.Load += it.Points
				placed[it.ID()] = si
				done = true
				break
			}
		}
		if !done {
			p.Overflow = append(p.Overflow, it)
		}
	}
	return p, nil
}

func itemFor(t Ticket, d Decision) Item {
	it := Item{
		Key: t.Key, Summary: t.Summary, EpicKey: t.EpicKey, Points: t.Points, Step: d.GoldenPathStep,
		Priority: t.Priority, Rule: d.Rule, Reason: d.Reason, BlockedBy: t.BlockedBy, rank: t.Rank,
	}
	if it.Points <= 0 {
		it.Points, it.Estimated = d.Estimate, true
	}
	return it
}

// schedule orders MVP items so blockers come first, then by golden-path
// step (non-golden-path work like legal and ops goes last), then priority,
// then the team's existing backlog rank.
func schedule(items map[string]Item) []Item {
	list := make([]Item, 0, len(items))
	for _, it := range items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if sa, sb := stepOrder(a.Step), stepOrder(b.Step); sa != sb {
			return sa < sb
		}
		if pa, pb := priorityOrder(a.Priority), priorityOrder(b.Priority); pa != pb {
			return pa < pb
		}
		return a.rank < b.rank
	})

	// Stable topological pass: emit an item only after its in-plan blockers.
	var out []Item
	emitted := map[string]bool{}
	var visit func(it Item)
	visiting := map[string]bool{}
	visit = func(it Item) {
		if emitted[it.ID()] || visiting[it.ID()] {
			return
		}
		visiting[it.ID()] = true
		for _, bk := range it.BlockedBy {
			if dep, ok := items[bk]; ok {
				visit(dep)
			}
		}
		emitted[it.ID()] = true
		out = append(out, it)
	}
	for _, it := range list {
		visit(it)
	}
	return out
}

func stepOrder(step int) int {
	if step <= 0 {
		return 6
	}
	return step
}

func priorityOrder(p string) int {
	switch p {
	case "Highest":
		return 0
	case "High":
		return 1
	case "Medium":
		return 2
	case "Low":
		return 3
	default:
		return 4
	}
}

func removeDecision(ds []Decision, key string) []Decision {
	out := ds[:0]
	for _, d := range ds {
		if d.Key != key {
			out = append(out, d)
		}
	}
	return out
}
