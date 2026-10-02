package jira

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// adfNode is an Atlassian Document Format node (Jira Cloud REST v3 rich text).
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfMark      `json:"marks"`
	Content []adfNode      `json:"content"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
}

// RichText converts a Jira rich-text field to Markdown: ADF (v3) is converted; a plain string (v2 wiki
// markup) is kept as written.
func RichText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc adfNode
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	return tidy(adfBlocks(doc.Content))
}

func attr(a map[string]any, k string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		return fmt.Sprint(int64(v))
	}
	return ""
}

func adfBlocks(ns []adfNode) string {
	var out []string
	var para strings.Builder
	flush := func() {
		if p := strings.TrimSpace(para.String()); p != "" {
			out = append(out, p)
		}
		para.Reset()
	}
	for _, n := range ns {
		if b, ok := adfBlock(n); ok {
			flush()
			if b = strings.TrimSpace(b); b != "" {
				out = append(out, b)
			}
			continue
		}
		para.WriteString(adfInline(n))
	}
	flush()
	return strings.Join(out, "\n\n")
}

// adfBlock renders block nodes; ok is false for inline nodes.
func adfBlock(n adfNode) (string, bool) {
	switch n.Type {
	case "paragraph":
		return adfInlines(n.Content), true
	case "heading":
		lvl := 1
		if l, ok := n.Attrs["level"].(float64); ok && l >= 1 && l <= 6 {
			lvl = int(l)
		}
		return strings.Repeat("#", lvl) + " " + strings.TrimSpace(adfInlines(n.Content)), true
	case "bulletList", "orderedList", "taskList":
		return adfList(n), true
	case "codeBlock":
		var b strings.Builder
		for _, c := range n.Content {
			b.WriteString(c.Text)
		}
		code := strings.Trim(b.String(), "\n")
		f := "```"
		for strings.Contains(code, f) {
			f += "`"
		}
		return f + attr(n.Attrs, "language") + "\n" + code + "\n" + f, true
	case "blockquote":
		return quote(adfBlocks(n.Content)), true
	case "panel":
		kind := attr(n.Attrs, "panelType")
		if kind == "" {
			kind = "info"
		}
		return quote("**" + strings.ToUpper(kind[:1]) + kind[1:] + ":**\n\n" + adfBlocks(n.Content)), true
	case "rule":
		return "---", true
	case "table":
		return adfTable(n), true
	case "expand", "nestedExpand":
		t := attr(n.Attrs, "title")
		if t == "" {
			t = "Details"
		}
		return "**" + t + "**\n\n" + adfBlocks(n.Content), true
	case "mediaSingle", "mediaGroup", "media":
		return "", true
	case "doc", "layoutSection", "layoutColumn", "bodiedExtension", "listItem", "taskItem", "decisionList":
		return adfBlocks(n.Content), true
	}
	return "", false
}

func adfList(n adfNode) string {
	var items []string
	for i, it := range n.Content {
		marker := "- "
		switch {
		case n.Type == "orderedList":
			marker = fmt.Sprintf("%d. ", i+1)
		case it.Type == "taskItem":
			if attr(it.Attrs, "state") == "DONE" {
				marker = "- [x] "
			} else {
				marker = "- [ ] "
			}
		}
		var lines []string
		for _, l := range strings.Split(adfBlocks(it.Content), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			lines = []string{""}
		}
		pad := strings.Repeat(" ", len(marker))
		for j := range lines {
			if j == 0 {
				lines[j] = marker + lines[j]
			} else {
				lines[j] = pad + lines[j]
			}
		}
		items = append(items, strings.Join(lines, "\n"))
	}
	return strings.Join(items, "\n")
}

func adfTable(n adfNode) string {
	var rows [][]string
	cols := 0
	for _, r := range n.Content {
		if r.Type != "tableRow" {
			continue
		}
		var row []string
		for _, c := range r.Content {
			cell := strings.Join(strings.Fields(strings.ReplaceAll(adfBlocks(c.Content), "\n", " ")), " ")
			row = append(row, strings.ReplaceAll(cell, "|", `\|`))
		}
		cols = max(cols, len(row))
		rows = append(rows, row)
	}
	if cols == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range rows {
		for len(r) < cols {
			r = append(r, "")
		}
		b.WriteString("| " + strings.Join(r, " | ") + " |\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", cols) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func adfInlines(ns []adfNode) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(adfInline(n))
	}
	return b.String()
}

func adfInline(n adfNode) string {
	switch n.Type {
	case "text":
		return marked(n.Text, n.Marks)
	case "hardBreak":
		return "\n"
	case "mention":
		t := attr(n.Attrs, "text")
		if t == "" {
			return "@user"
		}
		if !strings.HasPrefix(t, "@") {
			t = "@" + t
		}
		return t
	case "emoji":
		if t := attr(n.Attrs, "text"); t != "" {
			return t
		}
		return attr(n.Attrs, "shortName")
	case "inlineCard", "blockCard", "embedCard":
		return attr(n.Attrs, "url")
	case "status":
		return "[" + attr(n.Attrs, "text") + "]"
	case "date":
		if ms, ok := n.Attrs["timestamp"].(string); ok {
			var v int64
			if _, err := fmt.Sscan(ms, &v); err == nil {
				return time.UnixMilli(v).UTC().Format("2006-01-02")
			}
		}
		return ""
	}
	if b, ok := adfBlock(n); ok {
		return " " + strings.ReplaceAll(b, "\n", " ") + " "
	}
	return adfInlines(n.Content)
}

func marked(text string, marks []adfMark) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	var href string
	for _, m := range marks {
		switch m.Type {
		case "code":
			return "`" + text + "`"
		case "link":
			href = attr(m.Attrs, "href")
		}
	}
	lead, trail := "", ""
	if strings.HasPrefix(text, " ") {
		lead = " "
	}
	if strings.HasSuffix(text, " ") {
		trail = " "
	}
	t := strings.TrimSpace(text)
	for _, m := range marks {
		switch m.Type {
		case "strong":
			t = "**" + t + "**"
		case "em":
			t = "*" + t + "*"
		case "strike":
			t = "~~" + t + "~~"
		}
	}
	if href != "" && !strings.HasPrefix(href, "javascript:") {
		t = "[" + t + "](" + href + ")"
	}
	return lead + t + trail
}

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

var blankRE = regexp.MustCompile(`\n{3,}`)

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankRE.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
