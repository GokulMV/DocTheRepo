package confluence

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ToMarkdown converts Confluence storage format (XHTML with ac:/ri: elements) to Markdown for chunking:
// headings stay headings (they become chunk boundaries), code macros become fenced blocks, panels become
// quotes, tables stay tables, and links to other pages keep their titles. Presentation-only macros (table
// of contents, child lists) are dropped.
func ToMarkdown(storage string) string {
	root, err := parse(storage)
	if err != nil {
		return tidy(tagRE.ReplaceAllString(storage, " "))
	}
	return tidy(blocks(root))
}

var tagRE = regexp.MustCompile(`<[^>]*>`)

type node struct {
	name  string // "p", "ac:structured-macro"; "" for text
	attrs map[string]string
	kids  []*node
	text  string
}

func qname(n xml.Name) string {
	if n.Space != "" {
		return strings.ToLower(n.Space + ":" + n.Local)
	}
	return strings.ToLower(n.Local)
}

func parse(s string) (*node, error) {
	d := xml.NewDecoder(strings.NewReader("<root>" + s + "</root>"))
	// Only HTML void elements whose names cannot clash with ac:/ri: local names (ac:link, ac:parameter) auto-close.
	d.Strict, d.AutoClose, d.Entity = false, []string{"br", "hr", "img", "col", "area", "wbr", "input"}, xml.HTMLEntity
	root := &node{name: "root"}
	stack := []*node{root}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		top := stack[len(stack)-1]
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: qname(t.Name), attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[qname(a.Name)] = a.Value
			}
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top.kids = append(top.kids, &node{text: string(t)})
		}
	}
	return root, nil
}

var blockNames = map[string]bool{
	"p": true, "div": true, "section": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "pre": true, "table": true, "blockquote": true, "hr": true, "ac:task-list": true,
	"ac:layout": true, "ac:layout-section": true, "ac:layout-cell": true, "ac:rich-text-body": true, "root": true,
}

// inlineMacros render inside a line of text.
var inlineMacros = map[string]bool{"status": true, "jira": true, "anchor": true}

func isBlock(n *node) bool {
	if n.name == "ac:structured-macro" || n.name == "ac:macro" {
		return !inlineMacros[n.attrs["ac:name"]]
	}
	return blockNames[n.name]
}

// blocks renders n's children as Markdown blocks separated by blank lines.
func blocks(n *node) string {
	var out []string
	var para strings.Builder
	flush := func() {
		if p := cleanInline(para.String()); p != "" {
			out = append(out, p)
		}
		para.Reset()
	}
	for _, k := range n.kids {
		if k.name != "" && isBlock(k) {
			flush()
			if b := strings.TrimSpace(block(k)); b != "" {
				out = append(out, b)
			}
			continue
		}
		para.WriteString(inline(k))
	}
	flush()
	return strings.Join(out, "\n\n")
}

func block(n *node) string {
	switch n.name {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		t := cleanInline(strings.ReplaceAll(inlineKids(n), "\n", " "))
		if t == "" {
			return ""
		}
		return strings.Repeat("#", int(n.name[1]-'0')) + " " + t
	case "ul", "ol":
		return list(n, n.name == "ol")
	case "pre":
		return fence("", textOf(n))
	case "table":
		return table(n)
	case "blockquote":
		return quote(blocks(n))
	case "hr":
		return "---"
	case "ac:structured-macro", "ac:macro":
		return macro(n)
	case "ac:task-list":
		return tasks(n)
	}
	return blocks(n)
}

func list(n *node, ordered bool) string {
	var items []string
	i := 0
	for _, li := range n.kids {
		if li.name != "li" {
			continue
		}
		i++
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", i)
		}
		var lines []string
		for _, l := range strings.Split(blocks(li), "\n") {
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

func table(n *node) string {
	var rows [][]string
	var walk func(*node)
	walk = func(x *node) {
		for _, k := range x.kids {
			switch k.name {
			case "tr":
				var row []string
				for _, c := range k.kids {
					if c.name == "td" || c.name == "th" {
						cell := strings.Join(strings.Fields(strings.ReplaceAll(blocks(c), "\n", " ")), " ")
						row = append(row, strings.ReplaceAll(cell, "|", `\|`))
					}
				}
				rows = append(rows, row)
			case "thead", "tbody", "tfoot":
				walk(k)
			}
		}
	}
	walk(n)
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
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

func quote(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

func fence(lang, code string) string {
	code = strings.Trim(code, "\n")
	f := "```"
	for strings.Contains(code, f) {
		f += "`"
	}
	return f + lang + "\n" + code + "\n" + f
}

// macro renders a structured macro by name.
func macro(n *node) string {
	name := n.attrs["ac:name"]
	params := map[string]string{}
	var rich, plain *node
	for _, k := range n.kids {
		switch k.name {
		case "ac:parameter":
			params[k.attrs["ac:name"]] = strings.TrimSpace(textOf(k))
		case "ac:rich-text-body":
			rich = k
		case "ac:plain-text-body":
			plain = k
		}
	}
	body := func() string {
		switch {
		case rich != nil:
			return blocks(rich)
		case plain != nil:
			return strings.TrimSpace(textOf(plain))
		}
		return ""
	}
	switch name {
	case "code", "noformat":
		src := ""
		if plain != nil {
			src = textOf(plain)
		} else if rich != nil {
			src = textOf(rich)
		}
		return fence(params["language"], src)
	case "info", "note", "warning", "tip", "panel":
		label := strings.ToUpper(name[:1]) + name[1:]
		head := "**" + label + ":**"
		if t := params["title"]; t != "" {
			head += " " + t
		}
		if b := body(); b != "" {
			return quote(head + "\n\n" + b)
		}
		return quote(head)
	case "expand":
		t := params["title"]
		if t == "" {
			t = "Details"
		}
		return "**" + t + "**\n\n" + body()
	case "status":
		if t := params["title"]; t != "" {
			return "[" + t + "]"
		}
		return ""
	case "jira":
		if k := params["key"]; k != "" {
			return "Jira " + k
		}
		return ""
	case "toc", "children", "pagetree", "anchor", "recently-updated", "contentbylabel", "attachments", "livesearch",
		"include", "excerpt-include", "gallery", "profile", "space-details", "viewfile", "view-file":
		return ""
	}
	return body()
}

func tasks(n *node) string {
	var out []string
	for _, t := range n.kids {
		if t.name != "ac:task" {
			continue
		}
		done, body := false, ""
		for _, k := range t.kids {
			switch k.name {
			case "ac:task-status":
				done = strings.TrimSpace(textOf(k)) == "complete"
			case "ac:task-body":
				body = cleanInline(strings.ReplaceAll(inlineKids(k), "\n", " "))
			}
		}
		box := "[ ]"
		if done {
			box = "[x]"
		}
		out = append(out, "- "+box+" "+body)
	}
	return strings.Join(out, "\n")
}

func inlineKids(n *node) string {
	var b strings.Builder
	for _, k := range n.kids {
		b.WriteString(inline(k))
	}
	return b.String()
}

var spaceRE = regexp.MustCompile(`[ \t\r\n\f\x{00a0}]+`)

// inline renders a node inside a line of text.
func inline(n *node) string {
	if n.name == "" {
		return spaceRE.ReplaceAllString(n.text, " ")
	}
	switch n.name {
	case "strong", "b":
		return wrap(inlineKids(n), "**")
	case "em", "i":
		return wrap(inlineKids(n), "*")
	case "code", "tt":
		if t := strings.TrimSpace(textOf(n)); t != "" {
			return "`" + t + "`"
		}
		return ""
	case "s", "del", "strike":
		return wrap(inlineKids(n), "~~")
	case "br":
		return "\n"
	case "a":
		t := cleanInline(inlineKids(n))
		href := strings.TrimSpace(n.attrs["href"])
		switch {
		case href == "" || strings.HasPrefix(href, "javascript:"):
			return t
		case t == "":
			return href
		}
		return "[" + t + "](" + href + ")"
	case "img":
		return "![" + n.attrs["alt"] + "](" + n.attrs["src"] + ")"
	case "time":
		return n.attrs["datetime"]
	case "ac:link":
		return link(n)
	case "ac:image":
		for _, k := range n.kids {
			switch k.name {
			case "ri:attachment":
				return "[image: " + k.attrs["ri:filename"] + "]"
			case "ri:url":
				return "![](" + k.attrs["ri:value"] + ")"
			}
		}
		return ""
	case "ac:emoticon":
		return n.attrs["ac:emoji-fallback"]
	case "ac:placeholder", "ac:parameter", "ri:page", "ri:user", "ri:attachment", "ri:url", "ri:space":
		return ""
	case "ac:structured-macro", "ac:macro":
		return macro(n)
	}
	if isBlock(n) { // a block inside a table cell or link: keep its text on the line
		return " " + strings.ReplaceAll(blocks(n), "\n", " ") + " "
	}
	return inlineKids(n)
}

// link renders <ac:link>: the link body if any, else the target's title, file name, or user.
func link(n *node) string {
	var target *node
	body := ""
	for _, k := range n.kids {
		switch {
		case strings.HasPrefix(k.name, "ri:"):
			target = k
		case k.name == "ac:link-body":
			body = cleanInline(inlineKids(k))
		case k.name == "ac:plain-text-link-body":
			body = strings.TrimSpace(textOf(k))
		}
	}
	if body != "" {
		if target != nil && target.name == "ri:url" {
			return "[" + body + "](" + target.attrs["ri:value"] + ")"
		}
		return body
	}
	if target == nil {
		return n.attrs["ac:anchor"]
	}
	switch target.name {
	case "ri:page", "ri:blog-post":
		return target.attrs["ri:content-title"]
	case "ri:attachment":
		return target.attrs["ri:filename"]
	case "ri:url":
		return target.attrs["ri:value"]
	case "ri:space":
		return target.attrs["ri:space-key"]
	case "ri:user":
		return "@user"
	}
	return ""
}

func textOf(n *node) string {
	if n.name == "" {
		return n.text
	}
	var b strings.Builder
	for _, k := range n.kids {
		if k.name == "br" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(textOf(k))
	}
	return b.String()
}

// wrap applies an emphasis mark to the trimmed text, keeping surrounding spaces outside the mark.
func wrap(s, mark string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		if s != "" {
			return " "
		}
		return ""
	}
	lead, trail := "", ""
	if s[0] == ' ' {
		lead = " "
	}
	if s[len(s)-1] == ' ' {
		trail = " "
	}
	return lead + mark + t + mark + trail
}

// cleanInline trims each line of a paragraph and drops empty ones.
func cleanInline(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

var blankRE = regexp.MustCompile(`\n{3,}`)

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankRE.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
