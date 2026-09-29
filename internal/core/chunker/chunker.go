package chunker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// ID returns the stable chunk ID: sha256(scope + "::" + path + "::" + symbol)[:16]. Re-embedding the
// same symbol replaces its vector instead of duplicating it; moving it to another file changes the ID
// on purpose (the old ID is removed by the manifest diff).
func ID(scope, filePath, symbol string) string {
	sum := sha256.Sum256([]byte(scope + "::" + filePath + "::" + symbol))
	return hex.EncodeToString(sum[:8])
}

// Hash returns the content hash used to detect changed chunks.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// MaxFileBytes bounds what the chunker will read from one file; larger files are indexed by name only.
const MaxFileBytes = 1 << 20

// Code chunks a source file: one chunk per definition plus a __module__ chunk for top-level code.
func Code(scope, repoID, filePath string, a *FileAnalysis) []ports.Chunk {
	out := make([]ports.Chunk, 0, len(a.Definitions)+1)
	add := func(d Definition) {
		if strings.TrimSpace(d.Content) == "" {
			return
		}
		out = append(out, ports.Chunk{
			ID: ID(scope, filePath, d.Symbol), RepoID: repoID, Scope: scope, Source: ports.SourceCode,
			Path: filePath, Symbol: d.Symbol, Language: a.Language, Content: d.Content,
			ContentHash: Hash(d.Content), Signature: d.Signature, StartLine: d.StartLine, EndLine: d.EndLine,
		})
	}
	for _, d := range a.Definitions {
		add(d)
	}
	if a.Module != nil {
		add(*a.Module)
	}
	return out
}

// PlainText chunks a text file that has no grammar as a single whole-file chunk (capped), so it is still
// searchable. Binary content yields nothing.
func PlainText(scope, repoID, filePath string, src []byte) []ports.Chunk {
	if IsBinary(src) || len(bytes.TrimSpace(src)) == 0 {
		return nil
	}
	content := string(src)
	if len(content) > 16*1024 {
		content = content[:16*1024]
	}
	return []ports.Chunk{{
		ID: ID(scope, filePath, filePath), RepoID: repoID, Scope: scope, Source: ports.SourceCode,
		Path: filePath, Symbol: filePath, Language: "text", Content: content, ContentHash: Hash(content),
		StartLine: 1, EndLine: strings.Count(content, "\n") + 1,
	}}
}

// File chunks a source file with the registry, falling back to PlainText when no grammar exists.
// The analysis is returned so callers (triage, palace, scoped context) do not parse twice.
func File(reg *grammars.Registry, scope, repoID, filePath string, src []byte) ([]ports.Chunk, *FileAnalysis, error) {
	if len(src) > MaxFileBytes || IsBinary(src) {
		return nil, nil, nil
	}
	lang := reg.ForPath(filePath)
	if lang == nil {
		return PlainText(scope, repoID, filePath, src), nil, nil
	}
	a, err := Analyze(lang, src)
	if err != nil {
		return nil, nil, fmt.Errorf("analyze %s: %w", filePath, err)
	}
	return Code(scope, repoID, filePath, a), a, nil
}

// IsBinary reports whether content looks binary (a NUL byte in the first 8 KB, as git does).
func IsBinary(src []byte) bool {
	n := len(src)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(src[:n], 0) >= 0
}

// DocOptions tunes Markdown chunking.
type DocOptions struct {
	Source ports.ChunkSource
	URL    string
	// MaxTokens is the section size past which a section is sub-split (default ~900 tokens).
	MaxTokens int
	// OverlapTokens repeats the tail of one part at the start of the next (default 50).
	OverlapTokens int
}

var (
	headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	fenceRE   = regexp.MustCompile("^\\s*(```|~~~)")
	markerRE  = regexp.MustCompile(`<!--\s*dth:chunk\s+([0-9a-f]{16})\s*-->`)
)

type docSection struct {
	symbol    string
	body      strings.Builder
	startLine int
	endLine   int
}

// Docs chunks Markdown by H2/H3 section. Content before the first H2 is the "__intro__" chunk. The
// symbol is the heading path slug ("setup/install"), so editing a section's body keeps its ID stable.
// Sections past MaxTokens are split into "#partN" chunks with a small overlap.
func Docs(scope, repoID, filePath string, md []byte, o DocOptions) []ports.Chunk {
	if o.Source == "" {
		o.Source = ports.SourceGeneratedDoc
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = 900
	}
	if o.OverlapTokens < 0 || o.OverlapTokens >= o.MaxTokens {
		o.OverlapTokens = 50
	}
	lines := strings.Split(strings.ReplaceAll(string(md), "\r\n", "\n"), "\n")
	var sections []*docSection
	cur := &docSection{symbol: "__intro__", startLine: 1}
	var h2 string
	inFence := false
	for i, ln := range lines {
		if fenceRE.MatchString(ln) {
			inFence = !inFence
		}
		if !inFence {
			if m := headingRE.FindStringSubmatch(ln); m != nil && (len(m[1]) == 2 || len(m[1]) == 3) {
				sections = append(sections, cur)
				slug := Slug(m[2])
				if len(m[1]) == 2 {
					h2 = slug
					cur = &docSection{symbol: slug, startLine: i + 1}
				} else {
					sym := slug
					if h2 != "" {
						sym = h2 + "/" + slug
					}
					cur = &docSection{symbol: sym, startLine: i + 1}
				}
			}
		}
		cur.body.WriteString(ln)
		cur.body.WriteByte('\n')
		cur.endLine = i + 1
	}
	sections = append(sections, cur)

	seen := map[string]int{}
	var out []ports.Chunk
	for _, s := range sections {
		content := strings.TrimSpace(markerRE.ReplaceAllString(s.body.String(), ""))
		if content == "" {
			continue
		}
		sym := s.symbol
		seen[sym]++
		if n := seen[sym]; n > 1 {
			sym = fmt.Sprintf("%s#%d", sym, n) // repeated heading text in one file
		}
		for i, part := range splitByTokens(content, o.MaxTokens, o.OverlapTokens) {
			ps := sym
			if i > 0 {
				ps = fmt.Sprintf("%s#part%d", sym, i+1)
			}
			out = append(out, ports.Chunk{
				ID: ID(scope, filePath, ps), RepoID: repoID, Scope: scope, Source: o.Source, Path: filePath,
				Symbol: ps, Language: "markdown", Content: part, ContentHash: Hash(part), URL: o.URL,
				StartLine: s.startLine, EndLine: s.endLine,
			})
		}
	}
	return out
}

// EstimateTokens approximates tokens as ceil(chars/4) — the same estimator the spend guard uses.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// splitByTokens splits text on paragraph boundaries into parts of at most maxTokens (estimated), repeating
// roughly overlap tokens of the previous part's tail at the start of the next.
func splitByTokens(text string, maxTokens, overlap int) []string {
	if EstimateTokens(text) <= maxTokens {
		return []string{text}
	}
	maxChars, overlapChars := maxTokens*4, overlap*4
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, strings.TrimSpace(cur.String()))
			tail := cur.String()
			cur.Reset()
			if len(tail) > overlapChars {
				tail = tail[len(tail)-overlapChars:]
				if i := strings.IndexAny(tail, " \n"); i >= 0 {
					tail = tail[i+1:]
				}
			}
			cur.WriteString(tail)
		}
	}
	for _, para := range strings.SplitAfter(text, "\n\n") {
		for len(para) > maxChars { // a single enormous paragraph: hard-split on whitespace
			cut := strings.LastIndexAny(para[:maxChars], " \n")
			if cut <= 0 {
				cut = maxChars
			}
			if cur.Len() > overlapChars {
				flush()
			}
			cur.WriteString(para[:cut])
			flush()
			para = para[cut:]
		}
		if cur.Len()+len(para) > maxChars && cur.Len() > overlapChars {
			flush()
		}
		cur.WriteString(para)
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

// Slug lowercases a heading and keeps letters, digits, and single hyphens.
func Slug(h string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(h) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// RenameRekey maps every chunk ID of a renamed file to its ID under the new path, for a rename whose
// content is structurally unchanged: the vectors are copied to the new IDs and nothing is re-embedded.
func RenameRekey(scope, oldPath, newPath string, chunks []ports.Chunk) map[string]ports.Chunk {
	out := make(map[string]ports.Chunk, len(chunks))
	for _, c := range chunks {
		if c.Path != oldPath {
			continue
		}
		nc := c
		nc.Path = newPath
		if c.Symbol == oldPath { // whole-file text chunks are keyed by their path
			nc.Symbol = newPath
		}
		nc.ID = ID(scope, newPath, nc.Symbol)
		out[c.ID] = nc
	}
	return out
}

// IsMarkdown reports whether a path is a Markdown document.
func IsMarkdown(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}
