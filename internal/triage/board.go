// Package triage decides which backlog tickets belong in an MVP and fits
// them into the sprints left before a deadline.
package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/geekaara/algorithmic-triage/internal/jira"
)

type Ticket struct {
	Key         string
	Summary     string
	Description string
	Type        string
	Priority    string
	Status      string
	Points      float64 // 0 when unestimated
	EpicKey     string
	Epic        string
	Labels      []string
	Blocks      []string
	BlockedBy   []string
	Rank        int // backlog order
}

type SprintStat struct {
	ID        int
	Name      string
	Completed float64 // points done in the sprint
	Remaining float64 // points not done (active sprint only)
}

// Board is everything the engine knows about a project at one moment.
type Board struct {
	Project     string
	BoardID     int
	PointsField string
	Closed      []SprintStat // oldest first
	Active      *SprintStat
	Future      []jira.Sprint
	Candidates  []Ticket          // backlog and future-sprint tickets not yet triaged
	All         map[string]Ticket // every non-epic ticket, for dependency context
}

const triagedLabel = "ai-triaged"

func Load(ctx context.Context, c *jira.Client, project string) (*Board, error) {
	boardID, err := c.ScrumBoard(ctx, project)
	if err != nil {
		return nil, err
	}
	pointsField, err := c.EstimationField(ctx, boardID)
	if err != nil {
		return nil, err
	}
	b := &Board{Project: project, BoardID: boardID, PointsField: pointsField, All: map[string]Ticket{}}

	epics, err := c.SearchIssues(ctx, fmt.Sprintf("project = %s AND issuetype = Epic", project), []string{"summary"})
	if err != nil {
		return nil, err
	}
	epicName := map[string]string{}
	for _, e := range epics {
		epicName[e.Key] = str(e.Fields["summary"])
	}

	fields := []string{"summary", "description", "issuetype", "priority", "status", "labels", "parent", "issuelinks", pointsField}
	all, err := c.SearchIssues(ctx, fmt.Sprintf("project = %s AND issuetype != Epic ORDER BY Rank", project), fields)
	if err != nil {
		return nil, err
	}
	for i, is := range all {
		t := parseTicket(is, pointsField)
		t.Rank = i
		t.Epic = epicName[t.EpicKey]
		b.All[t.Key] = t
	}

	sprints, err := c.Sprints(ctx, boardID)
	if err != nil {
		return nil, err
	}
	sort.Slice(sprints, func(i, j int) bool { return sprints[i].ID < sprints[j].ID })
	for _, s := range sprints {
		switch s.State {
		case "closed", "active":
			in, err := c.SearchIssues(ctx, fmt.Sprintf("sprint = %d", s.ID), []string{"status", pointsField})
			if err != nil {
				return nil, err
			}
			st := SprintStat{ID: s.ID, Name: s.Name}
			for _, is := range in {
				pts := num(is.Fields[pointsField])
				if statusCategory(is.Fields["status"]) == "done" {
					st.Completed += pts
				} else {
					st.Remaining += pts
				}
			}
			if s.State == "closed" {
				b.Closed = append(b.Closed, st)
			} else {
				b.Active = &st
			}
		case "future":
			b.Future = append(b.Future, s)
		}
	}

	jql := fmt.Sprintf(`project = %s AND issuetype != Epic AND statusCategory != Done AND (sprint IS EMPTY OR sprint IN futureSprints()) AND (labels IS EMPTY OR labels NOT IN (%s)) ORDER BY Rank`, project, triagedLabel)
	cands, err := c.SearchIssues(ctx, jql, []string{"summary"})
	if err != nil {
		return nil, err
	}
	for _, is := range cands {
		b.Candidates = append(b.Candidates, b.All[is.Key])
	}
	return b, nil
}

func parseTicket(is jira.Issue, pointsField string) Ticket {
	t := Ticket{
		Key:         is.Key,
		Summary:     str(is.Fields["summary"]),
		Description: jira.PlainText(is.Fields["description"]),
		Type:        named(is.Fields["issuetype"]),
		Priority:    named(is.Fields["priority"]),
		Status:      named(is.Fields["status"]),
		Points:      num(is.Fields[pointsField]),
	}
	_ = json.Unmarshal(is.Fields["labels"], &t.Labels)
	var parent struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(is.Fields["parent"], &parent) == nil {
		t.EpicKey = parent.Key
	}

	var links []struct {
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
		InwardIssue  *struct{ Key string } `json:"inwardIssue"`
		OutwardIssue *struct{ Key string } `json:"outwardIssue"`
	}
	_ = json.Unmarshal(is.Fields["issuelinks"], &links)
	for _, l := range links {
		if l.Type.Name != "Blocks" {
			continue
		}
		if l.OutwardIssue != nil {
			t.Blocks = append(t.Blocks, l.OutwardIssue.Key)
		}
		if l.InwardIssue != nil {
			t.BlockedBy = append(t.BlockedBy, l.InwardIssue.Key)
		}
	}
	return t
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

func num(raw json.RawMessage) float64 {
	var f float64
	_ = json.Unmarshal(raw, &f)
	return f
}

func named(raw json.RawMessage) string {
	var v struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.Name
}

func statusCategory(raw json.RawMessage) string {
	var v struct {
		StatusCategory struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.StatusCategory.Key
}
