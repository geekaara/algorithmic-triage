package triage

import (
	"fmt"
	"regexp"
	"strings"
)

// Policy is the part of mvp.md a decision model can choose from: every line
// it might cite, as stable labels. A decision model can only pick from
// options, so it can only cite rules that actually exist.
type Policy struct {
	Steps []string          // golden-path steps, in order
	Lines map[string]string // label -> policy line, e.g. "out-of-scope-2"
	Order []string          // labels in document order
}

var (
	numbered = regexp.MustCompile(`^\d+\.\s+(.*)$`)
	bold     = regexp.MustCompile(`\*\*(.+?)\*\*`)
)

// ParsePolicy reads the golden path, non-negotiables, out-of-scope list and
// triage rules out of an mvp.md-style document.
func ParsePolicy(md string) Policy {
	p := Policy{Lines: map[string]string{}}
	section := ""
	add := func(prefix, text string) {
		text = strings.TrimSpace(bold.ReplaceAllString(text, "$1"))
		if text == "" {
			return
		}
		n := 1
		for _, l := range p.Order {
			if strings.HasPrefix(l, prefix+"-") {
				n++
			}
		}
		label := fmt.Sprintf("%s-%d", prefix, n)
		p.Lines[label] = text
		p.Order = append(p.Order, label)
	}

	// Collapse wrapped list items so each item is one line.
	var items []string
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "#"), t == "":
			items = append(items, t)
		case strings.HasPrefix(t, "- "), numbered.MatchString(t):
			items = append(items, t)
		case len(items) > 0 && items[len(items)-1] != "" && !strings.HasPrefix(items[len(items)-1], "#"):
			items[len(items)-1] += " " + t
		default:
			items = append(items, t)
		}
	}

	for _, t := range items {
		if strings.HasPrefix(t, "#") {
			h := strings.ToLower(t)
			switch {
			case strings.Contains(h, "golden path"):
				section = "golden"
			case strings.Contains(h, "non-negotiable"):
				section = "must"
			case strings.Contains(h, "out of scope"):
				section = "out-of-scope"
			case strings.Contains(h, "rules"):
				section = "rule"
			default:
				section = ""
			}
			continue
		}
		var text string
		if m := numbered.FindStringSubmatch(t); m != nil {
			text = m[1]
		} else if strings.HasPrefix(t, "- ") {
			text = strings.TrimPrefix(t, "- ")
		} else {
			continue
		}
		switch section {
		case "golden":
			p.Steps = append(p.Steps, strings.TrimSpace(bold.ReplaceAllString(text, "$1")))
			add("golden", text)
		case "must", "out-of-scope", "rule":
			add(section, text)
		}
	}
	return p
}

// Describe names a label for people, e.g. "Out of scope: AI / ML features…".
func (p Policy) Describe(label string) string {
	text, ok := p.Lines[label]
	if !ok {
		return label
	}
	kind := map[string]string{"golden": "Golden path", "must": "Non-negotiable", "out-of-scope": "Out of scope", "rule": "Triage rule"}
	prefix := label[:strings.LastIndex(label, "-")]
	return kind[prefix] + ": " + text
}
