package jira

import "strings"

// ADF converts a small Markdown subset into Atlassian Document Format,
// which the v3 API requires for rich-text fields such as description.
//
// Supported: "## " headings, "- " bullet lists, and blank-line-separated
// paragraphs. That is all ticket descriptions need.
func ADF(md string) map[string]any {
	var (
		content []any
		para    []string
		items   []any
	)
	flushPara := func() {
		if len(para) > 0 {
			content = append(content, block("paragraph", text(strings.Join(para, " "))))
			para = nil
		}
	}
	flushList := func() {
		if len(items) > 0 {
			content = append(content, map[string]any{"type": "bulletList", "content": items})
			items = nil
		}
	}

	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			flushPara()
			flushList()
		case strings.HasPrefix(line, "## "):
			flushPara()
			flushList()
			h := block("heading", text(strings.TrimPrefix(line, "## ")))
			h["attrs"] = map[string]any{"level": 3}
			content = append(content, h)
		case strings.HasPrefix(line, "- "):
			flushPara()
			item := block("paragraph", text(strings.TrimPrefix(line, "- ")))
			items = append(items, map[string]any{"type": "listItem", "content": []any{item}})
		default:
			flushList()
			para = append(para, line)
		}
	}
	flushPara()
	flushList()

	return map[string]any{"type": "doc", "version": 1, "content": content}
}

func block(typ string, children ...any) map[string]any {
	return map[string]any{"type": typ, "content": children}
}

func text(s string) map[string]any {
	return map[string]any{"type": "text", "text": s}
}
