package triage

import (
	"testing"

	"github.com/geekaara/algorithmic-triage/internal/jira"
)

func TestVelocity(t *testing.T) {
	tests := []struct {
		name   string
		closed []float64
		want   float64
	}{
		{"none", nil, 0},
		{"fewer than window", []float64{10, 20}, 15},
		{"trailing three", []float64{40, 18, 22, 20}, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stats []SprintStat
			for _, c := range tt.closed {
				stats = append(stats, SprintStat{Completed: c})
			}
			if got := Velocity(stats, 3); got != tt.want {
				t.Errorf("Velocity = %v, want %v", got, tt.want)
			}
		})
	}
}

func board(cands ...Ticket) *Board {
	b := &Board{
		Closed: []SprintStat{{Completed: 10}, {Completed: 10}, {Completed: 10}},
		Active: &SprintStat{Remaining: 4},
		Future: []jira.Sprint{{ID: 5, Name: "S5"}, {ID: 6, Name: "S6"}},
		All:    map[string]Ticket{},
	}
	for i, c := range cands {
		c.Rank = i
		b.Candidates = append(b.Candidates, c)
		b.All[c.Key] = c
	}
	return b
}

func TestBuildPlan_CapacityAndCarryOver(t *testing.T) {
	b := board(
		Ticket{Key: "A", Points: 5, Priority: "High"},
		Ticket{Key: "B", Points: 5, Priority: "High"},
		Ticket{Key: "C", Points: 5, Priority: "High"},
	)
	ds := map[string]Decision{
		"A": {Verdict: MVP, GoldenPathStep: 1},
		"B": {Verdict: MVP, GoldenPathStep: 2},
		"C": {Verdict: MVP, GoldenPathStep: 3},
	}
	p, err := BuildPlan(b, ds)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sprints[0].Capacity != 6 || p.Sprints[1].Capacity != 10 {
		t.Fatalf("capacities = %v, %v; want 6 (10 minus 4 carry-over), 10", p.Sprints[0].Capacity, p.Sprints[1].Capacity)
	}
	if !p.Fits() || p.Sprints[0].Load != 5 || p.Sprints[1].Load != 10 {
		t.Fatalf("loads = %v, %v, overflow %d", p.Sprints[0].Load, p.Sprints[1].Load, len(p.Overflow))
	}
}

func TestBuildPlan_OverflowIsReportedNotDropped(t *testing.T) {
	b := board(Ticket{Key: "A", Points: 13, Priority: "Highest"})
	p, err := BuildPlan(b, map[string]Decision{"A": {Verdict: MVP, GoldenPathStep: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Fits() || len(p.Overflow) != 1 || p.Overflow[0].Key != "A" {
		t.Fatalf("want A in overflow, got %+v", p.Overflow)
	}
}

func TestBuildPlan_BlockersArePulledIn(t *testing.T) {
	b := board(
		Ticket{Key: "UI", Points: 2, BlockedBy: []string{"API"}},
		Ticket{Key: "API", Points: 3, BlockedBy: []string{"DB"}},
		Ticket{Key: "DB", Points: 1},
	)
	ds := map[string]Decision{
		"UI":  {Verdict: MVP, GoldenPathStep: 4},
		"API": {Verdict: V2},
		"DB":  {Verdict: V2},
	}
	p, err := BuildPlan(b, ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.V2) != 0 {
		t.Fatalf("blockers should leave V2, got %+v", p.V2)
	}
	var got []string
	for _, it := range p.Sprints[0].Items {
		got = append(got, it.Key)
	}
	want := []string{"DB", "API", "UI"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestBuildPlan_SplitDoesNotInheritDependencies(t *testing.T) {
	b := board(
		Ticket{Key: "BIG", Points: 13, BlockedBy: []string{"ANALYTICS"}},
		Ticket{Key: "ANALYTICS", Points: 8},
	)
	ds := map[string]Decision{
		"BIG":       {Verdict: Split, SplitSummary: "Basic profile", SplitPoints: 2, GoldenPathStep: 1},
		"ANALYTICS": {Verdict: V2},
	}
	p, err := BuildPlan(b, ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.V2) != 1 || p.V2[0].Key != "ANALYTICS" {
		t.Fatalf("ANALYTICS should stay V2, got %+v", p.V2)
	}
	if it := p.Sprints[0].Items; len(it) != 1 || it[0].SplitFrom != "BIG" || it[0].Points != 2 {
		t.Fatalf("want the 2-point slice in sprint 1, got %+v", it)
	}
}

func TestBuildPlan_UnestimatedUsesModelEstimate(t *testing.T) {
	b := board(Ticket{Key: "BUG"})
	p, err := BuildPlan(b, map[string]Decision{"BUG": {Verdict: MVP, Estimate: 1, GoldenPathStep: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if it := p.Sprints[0].Items[0]; it.Points != 1 || !it.Estimated {
		t.Fatalf("got %+v", it)
	}
}

func TestBuildPlan_SplitBlockerIsNotPulledInTwice(t *testing.T) {
	b := board(
		Ticket{Key: "BALANCES", Points: 5},
		Ticket{Key: "SETTLE", Points: 2, BlockedBy: []string{"BALANCES"}},
	)
	b.Future = []jira.Sprint{{ID: 5, Name: "S5"}, {ID: 6, Name: "S6"}}
	ds := map[string]Decision{
		"BALANCES": {Verdict: Split, SplitSummary: "Basic balances", SplitPoints: 3, GoldenPathStep: 4},
		"SETTLE":   {Verdict: MVP, GoldenPathStep: 4},
	}
	p, err := BuildPlan(b, ds)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range p.Sprints {
		for _, it := range s.Items {
			ids = append(ids, it.ID())
		}
	}
	if len(ids) != 2 || ids[0] != "new:BALANCES" || ids[1] != "SETTLE" {
		t.Fatalf("want the slice then SETTLE, got %v", ids)
	}
	if got := p.MVPPoints(); got != 5 {
		t.Fatalf("MVP points = %v, want 5 (3-pt slice + 2)", got)
	}
}
