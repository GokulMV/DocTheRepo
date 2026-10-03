// Package docconv turns an uploaded file into Markdown for the Library: Markdown and plain text as they
// are, HTML converted. Other formats are refused with a clear message rather than guessed at.
package docconv

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// MaxBytes caps one uploaded file.
const MaxBytes = 5 << 20

// Supported lists the accepted extensions, for messages and the file picker.
var Supported = []string{".md", ".markdown", ".mdx", ".txt", ".text", ".rst", ".html", ".htm"}

// Convert returns the document's title and Markdown.
func Convert(name string, data []byte) (title, markdown string, err error) {
	if len(data) > MaxBytes {
		return "", "", fmt.Errorf("%s is larger than %d MB", name, MaxBytes>>20)
	}
	if !utf8.Valid(data) {
		return "", "", fmt.Errorf("%s is not UTF-8 text", name)
	}
	ext := strings.ToLower(path.Ext(name))
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	switch ext {
	case ".md", ".markdown", ".mdx", ".txt", ".text", ".rst":
		md := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
		return firstHeading(md, base), md, nil
	case ".html", ".htm":
		t, md := fromHTML(data)
		if t == "" {
			t = firstHeading(md, base)
		}
		return t, md, nil
	case ".pdf", ".docx", ".doc", ".pptx", ".xlsx":
		return "", "", fmt.Errorf("%s: %s files are not read yet; export it as Markdown, text or HTML first", name, ext)
	}
	return "", "", fmt.Errorf("%s: unsupported file type (use %s)", name, strings.Join(Supported, ", "))
}

var headingRE = regexp.MustCompile(`(?m)^#{1,2}\s+(.+?)\s*#*\s*$`)

func firstHeading(md, fallback string) string {
	if m := headingRE.FindStringSubmatch(md); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(fallback))
}

// fromHTML converts the common structure of an HTML page; scripts, styles and navigation are dropped.
func fromHTML(data []byte) (title, md string) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", strings.TrimSpace(string(data))
	}
	var b strings.Builder
	var walk func(n *html.Node, list string, depth int)
	text := func(n *html.Node) string {
		var b strings.Builder
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			b.WriteString(inline(k))
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}
	walk = func(n *html.Node, list string, depth int) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "head", "script", "style", "nav", "footer", "noscript", "svg":
				if n.Data == "head" {
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.Data == "title" && c.FirstChild != nil {
							title = strings.TrimSpace(c.FirstChild.Data)
						}
					}
				}
				return
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := int(n.Data[1] - '0')
				fmt.Fprintf(&b, "\n%s %s\n\n", strings.Repeat("#", level), text(n))
				return
			case "p":
				if s := text(n); s != "" {
					fmt.Fprintf(&b, "%s\n\n", s)
				}
				return
			case "pre":
				var raw strings.Builder
				var all func(*html.Node)
				all = func(c *html.Node) {
					if c.Type == html.TextNode {
						raw.WriteString(c.Data)
					}
					for k := c.FirstChild; k != nil; k = k.NextSibling {
						all(k)
					}
				}
				all(n)
				fmt.Fprintf(&b, "\n```\n%s\n```\n\n", strings.Trim(raw.String(), "\n"))
				return
			case "ul", "ol":
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, n.Data, depth+1)
				}
				b.WriteString("\n")
				return
			case "li":
				marker := "-"
				if list == "ol" {
					marker = "1."
				}
				fmt.Fprintf(&b, "%s%s %s\n", strings.Repeat("  ", max(depth-1, 0)), marker, text(n))
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "ul" || c.Data == "ol") {
						walk(c, c.Data, depth)
					}
				}
				return
			case "blockquote":
				fmt.Fprintf(&b, "> %s\n\n", text(n))
				return
			case "tr":
				var cells []string
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && (c.Data == "td" || c.Data == "th") {
						cells = append(cells, strings.ReplaceAll(text(c), "|", `\|`))
					}
				}
				fmt.Fprintf(&b, "| %s |\n", strings.Join(cells, " | "))
				return
			case "hr":
				b.WriteString("\n---\n\n")
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, list, depth)
		}
	}
	walk(doc, "", 0)
	md = regexp.MustCompile(`\n{3,}`).ReplaceAllString(b.String(), "\n\n")
	return title, strings.TrimSpace(md)
}

// inline renders a node's inline content as Markdown.
func inline(c *html.Node) string {
	kids := func() string {
		var b strings.Builder
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			b.WriteString(inline(k))
		}
		return b.String()
	}
	switch {
	case c.Type == html.TextNode:
		return c.Data
	case c.Type != html.ElementNode:
		return kids()
	}
	switch c.Data {
	case "script", "style":
		return ""
	case "br":
		return "\n"
	case "strong", "b":
		return wrap("**", kids())
	case "em", "i":
		return wrap("*", kids())
	case "code":
		return wrap("`", kids())
	case "a":
		label, href := strings.TrimSpace(kids()), attr(c, "href")
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") || label == "" {
			return label
		}
		return "[" + label + "](" + href + ")"
	}
	return kids()
}

func wrap(mark, s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	return mark + strings.TrimSpace(s) + mark
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
