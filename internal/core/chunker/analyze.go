// Package chunker turns source files and Markdown into stable, individually addressable chunks, and
// exposes the per-file syntax analysis (definitions, calls, imports, env reads, annotations) that triage,
// scoped context, and the palace extractor share (plan § 8.1, § 8.2, § 8.5, § 8.6).
package chunker

import (
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
)

// ModuleSymbol names the pseudo-definition holding top-level code outside any definition.
const ModuleSymbol = "__module__"

// Call is one call expression.
type Call struct {
	Callee     string   `json:"callee"` // full callee text, e.g. "r.Get" or "os.Getenv"
	Name       string   `json:"name"`   // last segment, e.g. "Get"
	Line       int      `json:"line"`
	StringArgs []string `json:"string_args,omitempty"`
}

// Annotation is a decorator/annotation attached to a definition.
type Annotation struct {
	Name       string   `json:"name"` // e.g. "GetMapping", "app.get", "get"
	StringArgs []string `json:"string_args,omitempty"`
	Text       string   `json:"text"`
	Line       int      `json:"line"`
}

// EnvRead is an environment-variable access.
type EnvRead struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// Definition is one chunkable declaration.
type Definition struct {
	Symbol      string       `json:"symbol"` // qualified, e.g. "OrderService.refund"
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Parent      string       `json:"parent,omitempty"`
	StartByte   uint         `json:"start_byte"`
	EndByte     uint         `json:"end_byte"`
	StartLine   int          `json:"start_line"` // 1-based
	EndLine     int          `json:"end_line"`
	Signature   string       `json:"signature"`
	Content     string       `json:"content"`
	Calls       []Call       `json:"calls,omitempty"`
	Annotations []Annotation `json:"annotations,omitempty"`
	EnvReads    []EnvRead    `json:"env_reads,omitempty"`
	params      string
}

// Import is one import statement.
type Import struct {
	Path  string   `json:"path"`            // module path / source string as written
	Alias string   `json:"alias,omitempty"` // local alias, if any
	Names []string `json:"names,omitempty"` // imported names (from x import a, b)
	Line  int      `json:"line"`
	Text  string   `json:"text"`
}

// FileAnalysis is everything the core needs from one parsed file.
type FileAnalysis struct {
	Language    string       `json:"language"`
	Definitions []Definition `json:"definitions"`
	// Module holds top-level code outside definitions (package vars, module-level calls such as
	// Express route registration). Nil when the file has none.
	Module    *Definition `json:"module,omitempty"`
	Imports   []Import    `json:"imports,omitempty"`
	ImportBlk string      `json:"import_block,omitempty"`
	// Tokens is the normalized structural token stream used by triage: comments dropped, cosmetic string
	// contents collapsed, whitespace ignored, nesting preserved.
	Tokens    []string `json:"-"`
	HasErrors bool     `json:"has_errors"`
}

// Analyze parses src with lang and extracts its structure.
func Analyze(lang *grammars.Language, src []byte) (*FileAnalysis, error) {
	tree, err := lang.Parse(src)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	root := tree.RootNode()
	a := &analyzer{lang: lang, src: src, out: &FileAnalysis{Language: lang.Name, HasErrors: root.HasError()}, handled: map[uintptr]bool{}}
	a.module = &Definition{Symbol: ModuleSymbol, Name: ModuleSymbol, Kind: "module"}
	a.walk(root, "", a.module)
	a.tokens(root, false)
	a.finishModule(root)
	a.dedupe()
	return a.out, nil
}

type analyzer struct {
	lang    *grammars.Language
	src     []byte
	out     *FileAnalysis
	module  *Definition
	handled map[uintptr]bool
	modText []string
	imports []string
}

func (a *analyzer) text(n *sitter.Node) string { return n.Utf8Text(a.src) }

func line(n *sitter.Node) int { return int(n.StartPosition().Row) + 1 }

// walk visits n, collecting definitions, calls, env reads, and imports. cur receives calls/env reads.
func (a *analyzer) walk(n *sitter.Node, prefix string, cur *Definition) {
	kind := n.Kind()
	if a.lang.IsComment(kind) {
		return
	}
	if a.lang.IsImport(kind) {
		a.collectImport(n)
		return
	}
	if a.handled[n.Id()] {
		return
	}
	// A decorator/attribute directly before a definition belongs to that definition's chunk.
	if a.lang.IsAnnotation(kind) && a.annotatesNextDefinition(n) {
		return
	}
	if def, chunkNode, inner := a.definitionAt(n); def != nil {
		a.buildDefinition(def, chunkNode, inner, prefix, cur)
		return
	}
	a.observe(n, cur)
	for i := uint(0); i < n.ChildCount(); i++ {
		a.walk(n.Child(i), prefix, cur)
	}
}

// annotatesNextDefinition reports whether the next sibling after any further comments/annotations is a
// definition, without the side effects of definitionAt.
func (a *analyzer) annotatesNextDefinition(n *sitter.Node) bool {
	for s := n.NextSibling(); s != nil; s = s.NextSibling() {
		k := s.Kind()
		if a.lang.IsComment(k) || a.lang.IsAnnotation(k) {
			continue
		}
		return a.isDefinitionStart(s)
	}
	return false
}

func (a *analyzer) isDefinitionStart(n *sitter.Node) bool {
	k := n.Kind()
	if field, ok := a.lang.Wrappers[k]; ok {
		inner := n.ChildByFieldName(field)
		return inner != nil && a.isDefinitionStart(inner)
	}
	if _, ok := a.lang.Definitions[k]; ok {
		return true
	}
	if a.lang.IsFunctionDeclarator(k) {
		v := n.ChildByFieldName("value")
		return v != nil && a.lang.IsFunctionValue(v.Kind())
	}
	if k == "lexical_declaration" && n.NamedChildCount() == 1 {
		return a.isDefinitionStart(n.NamedChild(0))
	}
	return false
}

// definitionAt reports whether n starts a definition, returning the definition kind, the node whose text
// forms the chunk, and the node that carries name/body fields.
func (a *analyzer) definitionAt(n *sitter.Node) (*Definition, *sitter.Node, *sitter.Node) {
	if a.handled[n.Id()] {
		return nil, nil, nil
	}
	kind := n.Kind()
	if field, ok := a.lang.Wrappers[kind]; ok {
		if inner := n.ChildByFieldName(field); inner != nil {
			if d, _, in := a.definitionAt(inner); d != nil {
				a.handled[inner.Id()] = true
				if in != nil && in.Id() != inner.Id() {
					a.handled[in.Id()] = true
				}
				return d, n, in
			}
		}
		return nil, nil, nil
	}
	if dk, ok := a.lang.Definitions[kind]; ok {
		chunk := n
		// Go: a single type_spec's chunk is its whole type_declaration (keeps "type" and doc comments).
		if kind == "type_spec" {
			if p := n.Parent(); p != nil && p.Kind() == "type_declaration" && p.NamedChildCount() == 1 {
				chunk = p
			}
		}
		return &Definition{Kind: dk}, chunk, n
	}
	// `const f = () => {}` / `export const f = ...`: a declaration holding one function declarator.
	if (kind == "lexical_declaration" || kind == "variable_declaration") && n.NamedChildCount() == 1 {
		if d := n.NamedChild(0); a.lang.IsFunctionDeclarator(d.Kind()) {
			if v := d.ChildByFieldName("value"); v != nil && a.lang.IsFunctionValue(v.Kind()) {
				a.handled[d.Id()] = true
				return &Definition{Kind: "function"}, n, d
			}
		}
	}
	if a.lang.IsFunctionDeclarator(kind) {
		if v := n.ChildByFieldName("value"); v != nil && a.lang.IsFunctionValue(v.Kind()) {
			chunk := n
			if p := n.Parent(); p != nil && p.NamedChildCount() == 1 {
				chunk = p // `const f = () => {}`: include the const keyword
			}
			return &Definition{Kind: "function"}, chunk, n
		}
	}
	return nil, nil, nil
}

func (a *analyzer) buildDefinition(d *Definition, chunk, inner *sitter.Node, prefix string, outer *Definition) {
	a.handled[chunk.Id()] = true
	d.Name = a.nameOf(inner)
	if d.Name == "" {
		d.Name = "anonymous@" + strconv.Itoa(line(inner))
	}
	childPrefix := d.Name
	switch inner.Kind() {
	case "method_declaration":
		// Go methods are qualified by receiver type.
		if recv := inner.ChildByFieldName("receiver"); recv != nil {
			if t := receiverType(a, recv); t != "" {
				d.Name = t + "." + d.Name
			}
		}
	case "impl_item":
		typ := ""
		if t := inner.ChildByFieldName("type"); t != nil {
			typ = a.text(t)
		}
		childPrefix = typ
		if tr := inner.ChildByFieldName("trait"); tr != nil {
			d.Name = "impl " + a.text(tr) + " for " + typ
		} else {
			d.Name = "impl " + typ
		}
	}
	d.Symbol = d.Name
	if prefix != "" {
		d.Symbol = prefix + "." + d.Name
		d.Parent = prefix
	}

	start := a.leadingStart(chunk)
	d.StartByte, d.EndByte = start.StartByte(), chunk.EndByte()
	d.StartLine, d.EndLine = line(start), int(chunk.EndPosition().Row)+1
	d.Content = string(a.src[d.StartByte:d.EndByte])
	sigStart := chunk.StartByte()
	for s := start; s != nil && s.Id() != chunk.Id(); s = s.NextSibling() {
		if a.lang.IsAnnotation(s.Kind()) {
			sigStart = s.StartByte() // decorators are part of the signature; doc comments are not
			break
		}
	}
	d.Signature = a.signature(sigStart, chunk, inner)
	if p := inner.ChildByFieldName("parameters"); p != nil {
		d.params = strings.Join(strings.Fields(a.text(p)), " ")
	}
	a.collectAnnotations(d, start, chunk, inner)
	for s := start; s != nil && s.Id() != chunk.Id(); s = s.NextSibling() {
		a.handled[s.Id()] = true
		a.observeTree(s, d)
	}

	body := a.bodyOf(inner)
	nestedPrefix := ""
	if a.lang.IsScope(inner.Kind()) {
		nestedPrefix = childPrefix
		if prefix != "" {
			nestedPrefix = prefix + "." + childPrefix
		}
	}
	// Everything but the body is observed for calls/env reads (e.g. default arguments, annotations).
	a.observeExcept(chunk, body, d)
	a.out.Definitions = append(a.out.Definitions, *d)
	idx := len(a.out.Definitions) - 1
	if body != nil {
		if nestedPrefix != "" {
			// Scope: nested definitions become their own chunks; loose statements belong to this one.
			holder := &a.out.Definitions[idx]
			tmp := *holder
			for i := uint(0); i < body.ChildCount(); i++ {
				a.walk(body.Child(i), nestedPrefix, &tmp)
			}
			a.out.Definitions[idx].Calls = tmp.Calls
			a.out.Definitions[idx].EnvReads = tmp.EnvReads
			a.outline(idx, nestedPrefix)
		} else {
			tmp := a.out.Definitions[idx]
			a.observeTree(body, &tmp)
			a.out.Definitions[idx].Calls = tmp.Calls
			a.out.Definitions[idx].EnvReads = tmp.EnvReads
		}
	}
	_ = outer
}

// outline replaces the bodies of a scope's direct children with their signatures, so a class/impl chunk
// describes its shape without duplicating every method (each method is its own chunk).
func (a *analyzer) outline(idx int, childPrefix string) {
	parent := &a.out.Definitions[idx]
	var b strings.Builder
	pos := parent.StartByte
	for i := idx + 1; i < len(a.out.Definitions); i++ {
		c := a.out.Definitions[i]
		if c.Parent != childPrefix || c.StartByte < pos || c.EndByte > parent.EndByte {
			continue
		}
		b.Write(a.src[pos:c.StartByte])
		b.WriteString(c.Signature)
		b.WriteString(" …")
		pos = c.EndByte
	}
	if pos == parent.StartByte {
		return // no nested definitions: keep the full text
	}
	b.Write(a.src[pos:parent.EndByte])
	parent.Content = b.String()
}

// observeExcept is observeTree that skips one subtree (the body, observed separately).
func (a *analyzer) observeExcept(n, skip *sitter.Node, d *Definition) {
	if skip != nil && n.Id() == skip.Id() || a.lang.IsComment(n.Kind()) {
		return
	}
	a.observe(n, d)
	for i := uint(0); i < n.ChildCount(); i++ {
		a.observeExcept(n.Child(i), skip, d)
	}
}

// observeTree records calls/env reads in n and all descendants, without creating definitions.
func (a *analyzer) observeTree(n *sitter.Node, d *Definition) {
	if a.lang.IsComment(n.Kind()) {
		return
	}
	a.observe(n, d)
	for i := uint(0); i < n.ChildCount(); i++ {
		a.observeTree(n.Child(i), d)
	}
}

// observe records a call or env read at n itself.
func (a *analyzer) observe(n *sitter.Node, d *Definition) {
	kind := n.Kind()
	if field, ok := a.lang.Calls[kind]; ok {
		callee := n.ChildByFieldName(field)
		if callee == nil {
			return
		}
		full := a.text(callee)
		if kind == "method_invocation" { // Java: object.name(...)
			if obj := n.ChildByFieldName("object"); obj != nil {
				full = a.text(obj) + "." + full
			}
		}
		if kind == "macro_invocation" {
			full += "!"
		}
		c := Call{Callee: compact(full), Name: lastSegment(full), Line: line(n)}
		if args := n.ChildByFieldName(a.lang.CallArgsField); args != nil {
			c.StringArgs = a.stringArgs(args)
		} else if kind == "macro_invocation" {
			c.StringArgs = a.stringArgs(n)
		}
		d.Calls = append(d.Calls, c)
		for _, ec := range a.lang.EnvCallees {
			if c.Callee == ec && len(c.StringArgs) > 0 {
				d.EnvReads = append(d.EnvReads, EnvRead{Name: c.StringArgs[0], Line: c.Line})
			}
		}
		return
	}
	if len(a.lang.EnvObjects) == 0 {
		return
	}
	var obj, key *sitter.Node
	switch kind {
	case "member_expression": // process.env.KEY
		obj, key = n.ChildByFieldName("object"), n.ChildByFieldName("property")
	case "subscript_expression": // process.env["KEY"]
		obj, key = n.ChildByFieldName("object"), n.ChildByFieldName("index")
	case "subscript": // os.environ["KEY"]
		obj, key = n.ChildByFieldName("value"), n.ChildByFieldName("subscript")
	default:
		return
	}
	if obj == nil || key == nil {
		return
	}
	ot := compact(a.text(obj))
	for _, eo := range a.lang.EnvObjects {
		if ot == eo {
			name := a.text(key)
			if a.lang.IsString(key.Kind()) {
				name = unquote(name)
			}
			if name != "" {
				d.EnvReads = append(d.EnvReads, EnvRead{Name: name, Line: line(n)})
			}
		}
	}
}

func (a *analyzer) stringArgs(args *sitter.Node) []string {
	var out []string
	for i := uint(0); i < args.NamedChildCount(); i++ {
		c := args.NamedChild(i)
		if a.lang.IsString(c.Kind()) {
			out = append(out, unquote(a.text(c)))
		} else if c.Kind() == "keyword_argument" || c.Kind() == "element_value_pair" || c.Kind() == "pair" {
			// Python f(path="x"), Java @A(value="x"), JS {path: "x"}: take the value when it is a string.
			if v := c.ChildByFieldName("value"); v != nil && a.lang.IsString(v.Kind()) {
				out = append(out, unquote(a.text(v)))
			}
		} else if c.Kind() == "token_tree" { // Rust macro arguments
			out = append(out, a.stringArgs(c)...)
		}
	}
	return out
}

func (a *analyzer) nameOf(n *sitter.Node) string {
	for _, f := range a.lang.NameFields {
		if c := n.ChildByFieldName(f); c != nil {
			return compact(a.text(c))
		}
	}
	return ""
}

func receiverType(a *analyzer, recv *sitter.Node) string {
	var find func(n *sitter.Node) string
	find = func(n *sitter.Node) string {
		if n.Kind() == "type_identifier" {
			return a.text(n)
		}
		for i := uint(0); i < n.NamedChildCount(); i++ {
			if t := find(n.NamedChild(i)); t != "" {
				return t
			}
		}
		return ""
	}
	return find(recv)
}

func (a *analyzer) bodyOf(n *sitter.Node) *sitter.Node {
	for _, f := range a.lang.BodyFields {
		if b := n.ChildByFieldName(f); b != nil {
			return b
		}
	}
	if v := n.ChildByFieldName("value"); v != nil && a.lang.IsFunctionValue(v.Kind()) {
		return v.ChildByFieldName("body")
	}
	return nil
}

// leadingStart extends a chunk backwards over directly preceding comments and annotations (doc comments,
// Rust #[attributes], TS method decorators) so they travel with the definition.
func (a *analyzer) leadingStart(n *sitter.Node) *sitter.Node {
	start := n
	for {
		prev := start.PrevSibling()
		if prev == nil {
			return start
		}
		k := prev.Kind()
		if !a.lang.IsComment(k) && !a.lang.IsAnnotation(k) {
			return start
		}
		// Stop at a blank line: a comment separated by an empty line is not attached documentation.
		if int(start.StartPosition().Row)-int(prev.EndPosition().Row) > 1 {
			return start
		}
		start = prev
	}
}

func (a *analyzer) signature(from uint, chunk, inner *sitter.Node) string {
	body := a.bodyOf(inner)
	var sig string
	if body != nil {
		sig = string(a.src[from:body.StartByte()])
	} else {
		sig = string(a.src[from:chunk.EndByte()])
		if len(sig) > 400 {
			if i := strings.IndexByte(sig, '\n'); i > 0 {
				sig = sig[:i]
			} else {
				sig = sig[:400]
			}
		}
	}
	sig = strings.TrimRight(strings.TrimSpace(sig), "{:= ")
	return strings.TrimSpace(sig)
}

func (a *analyzer) collectAnnotations(d *Definition, start, chunk, inner *sitter.Node) {
	body := a.bodyOf(inner)
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if body != nil && n.Id() == body.Id() {
			return
		}
		if a.lang.IsAnnotation(n.Kind()) {
			ann := Annotation{Text: compact(a.text(n)), Line: line(n)}
			if nm := n.ChildByFieldName("name"); nm != nil {
				ann.Name = a.text(nm)
			} else {
				ann.Name = annotationName(ann.Text)
			}
			ann.StringArgs = a.allStrings(n)
			d.Annotations = append(d.Annotations, ann)
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			visit(n.Child(i))
		}
	}
	for s := start; s != nil && s.Id() != chunk.Id(); s = s.NextSibling() {
		visit(s)
	}
	visit(chunk)
}

func (a *analyzer) allStrings(n *sitter.Node) []string {
	var out []string
	var visit func(n *sitter.Node)
	visit = func(n *sitter.Node) {
		if a.lang.IsString(n.Kind()) {
			out = append(out, unquote(a.text(n)))
			return
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			visit(n.Child(i))
		}
	}
	visit(n)
	return out
}

func (a *analyzer) collectImport(n *sitter.Node) {
	a.imports = append(a.imports, a.text(n))
	strs := a.allStrings(n)
	txt := compact(a.text(n))
	switch a.lang.Name {
	case "go":
		// Each import_spec: optional name + path.
		var visit func(n *sitter.Node)
		visit = func(n *sitter.Node) {
			if n.Kind() == "import_spec" {
				imp := Import{Line: line(n), Text: compact(a.text(n))}
				if p := n.ChildByFieldName("path"); p != nil {
					imp.Path = unquote(a.text(p))
				}
				if nm := n.ChildByFieldName("name"); nm != nil {
					imp.Alias = a.text(nm)
				}
				a.out.Imports = append(a.out.Imports, imp)
				return
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				visit(n.NamedChild(i))
			}
		}
		visit(n)
	case "python":
		imp := Import{Line: line(n), Text: txt}
		if mn := n.ChildByFieldName("module_name"); mn != nil {
			imp.Path = a.text(mn)
			for i := uint(0); i < n.NamedChildCount(); i++ {
				c := n.NamedChild(i)
				if c.Id() != mn.Id() && (c.Kind() == "dotted_name" || c.Kind() == "aliased_import") {
					imp.Names = append(imp.Names, lastSegment(a.text(c)))
				}
			}
		} else if nm := n.ChildByFieldName("name"); nm != nil {
			imp.Path = a.text(nm)
		}
		a.out.Imports = append(a.out.Imports, imp)
	case "java":
		sp := strings.Join(strings.Fields(a.text(n)), " ")
		p := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(sp, "import "), "static "), ";")
		a.out.Imports = append(a.out.Imports, Import{Path: strings.TrimSpace(p), Names: []string{lastSegment(p)}, Line: line(n), Text: txt})
	case "rust":
		sp := strings.Join(strings.Fields(a.text(n)), " ")
		p := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(sp, "pub "), "use "), ";")
		a.out.Imports = append(a.out.Imports, Import{Path: strings.TrimSpace(p), Names: []string{lastSegment(p)}, Line: line(n), Text: txt})
	default: // typescript/javascript and runtime grammars: the source string is the path
		imp := Import{Line: line(n), Text: txt}
		if src := n.ChildByFieldName("source"); src != nil {
			imp.Path = unquote(a.text(src))
		} else if len(strs) > 0 {
			imp.Path = strs[len(strs)-1]
		}
		var names func(n *sitter.Node)
		names = func(n *sitter.Node) {
			if n.Kind() == "import_specifier" || n.Kind() == "namespace_import" {
				if nm := n.ChildByFieldName("name"); nm != nil {
					imp.Names = append(imp.Names, a.text(nm))
				} else {
					imp.Names = append(imp.Names, lastSegment(a.text(n)))
				}
				return
			}
			if n.Kind() == "import_clause" && n.NamedChildCount() > 0 && n.NamedChild(0).Kind() == "identifier" {
				imp.Names = append(imp.Names, a.text(n.NamedChild(0)))
			}
			for i := uint(0); i < n.NamedChildCount(); i++ {
				names(n.NamedChild(i))
			}
		}
		names(n)
		a.out.Imports = append(a.out.Imports, imp)
	}
}

// finishModule builds the __module__ pseudo-definition from top-level code outside definitions,
// excluding package/import statements and comments.
func (a *analyzer) finishModule(root *sitter.Node) {
	a.out.ImportBlk = strings.Join(a.imports, "\n")
	var parts []string
	for i := uint(0); i < root.NamedChildCount(); i++ {
		c := root.NamedChild(i)
		k := c.Kind()
		if a.handled[c.Id()] || a.lang.IsComment(k) || a.lang.IsImport(k) || k == "package_clause" ||
			k == "package_declaration" || isDefinitionContainer(a, c) {
			continue
		}
		parts = append(parts, a.text(c))
	}
	if len(parts) == 0 && len(a.module.Calls) == 0 {
		return
	}
	m := a.module
	m.Content = strings.Join(parts, "\n")
	m.Signature = ModuleSymbol
	m.StartLine, m.EndLine = 1, int(root.EndPosition().Row)+1
	if strings.TrimSpace(m.Content) == "" && len(m.Calls) == 0 && len(m.EnvReads) == 0 {
		return
	}
	a.out.Module = m
}

// isDefinitionContainer reports top-level nodes that only exist to hold definitions already chunked
// (a Go type_declaration with several specs, a TS export wrapping a definition).
func isDefinitionContainer(a *analyzer, n *sitter.Node) bool {
	if n.NamedChildCount() == 0 {
		return false
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(i)
		if !a.handled[c.Id()] && !a.lang.IsComment(c.Kind()) {
			return false
		}
	}
	return true
}

// tokens emits the normalized structural stream (see FileAnalysis.Tokens).
func (a *analyzer) tokens(n *sitter.Node, significant bool) {
	kind := n.Kind()
	if a.lang.IsComment(kind) {
		return
	}
	if a.lang.IsSignificantStringParent(kind) || a.lang.IsAnnotation(kind) {
		significant = true
	}
	if a.lang.IsString(kind) {
		if significant || a.isEnvArg(n) {
			a.out.Tokens = append(a.out.Tokens, "s:"+a.text(n))
		} else {
			a.out.Tokens = append(a.out.Tokens, "s")
		}
		return
	}
	// A statement that is only a string literal (docstring) is a no-op: treat as a comment.
	if kind == "expression_statement" && n.NamedChildCount() == 1 && a.lang.IsString(n.NamedChild(0).Kind()) {
		return
	}
	if n.ChildCount() == 0 {
		a.out.Tokens = append(a.out.Tokens, a.text(n))
		return
	}
	if n.IsNamed() {
		a.out.Tokens = append(a.out.Tokens, "("+kind)
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		a.tokens(n.Child(i), significant)
	}
	if n.IsNamed() {
		a.out.Tokens = append(a.out.Tokens, ")")
	}
}

// isEnvArg reports whether a string literal is the name argument of an env-var getter: renaming the
// variable an app reads is a configuration change, never cosmetic.
func (a *analyzer) isEnvArg(n *sitter.Node) bool {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if field, ok := a.lang.Calls[p.Kind()]; ok {
			if c := p.ChildByFieldName(field); c != nil {
				callee := compact(a.text(c))
				if p.Kind() == "macro_invocation" {
					callee += "!"
				}
				for _, ec := range a.lang.EnvCallees {
					if callee == ec {
						return true
					}
				}
			}
			return false
		}
		switch p.Kind() {
		case "subscript", "subscript_expression":
			return true // os.environ["K"] / process.env["K"]; indexing by a literal is structural anyway
		}
	}
	return false
}

func (a *analyzer) dedupe() {
	seen := map[string]int{}
	for _, d := range a.out.Definitions {
		seen[d.Symbol]++
	}
	used := map[string]int{}
	for i := range a.out.Definitions {
		d := &a.out.Definitions[i]
		if seen[d.Symbol] > 1 && d.params != "" {
			d.Symbol = d.Symbol + d.params
		}
		used[d.Symbol]++
		if n := used[d.Symbol]; n > 1 {
			d.Symbol = d.Symbol + "#" + strconv.Itoa(n)
		}
	}
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }

func lastSegment(s string) string {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"::", "->", ".", "/"} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return strings.TrimSuffix(s, ";")
}

func annotationName(text string) string {
	t := strings.TrimLeft(text, "@#[")
	if i := strings.IndexAny(t, "(]"); i >= 0 {
		t = t[:i]
	}
	return t
}

// unquote strips string delimiters, including Python prefixes and triple quotes, JS template ticks,
// and Rust raw strings.
func unquote(s string) string {
	s = strings.TrimLeft(s, "rbuRBUf")
	s = strings.TrimLeft(s, "#")
	for _, q := range []string{`"""`, `'''`, `"`, `'`, "`"} {
		if len(s) >= 2*len(q) && strings.HasPrefix(s, q) {
			s = strings.TrimSuffix(strings.TrimRight(s, "#"), q)
			return s[len(q):]
		}
	}
	return s
}
