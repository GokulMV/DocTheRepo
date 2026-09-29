package chunker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var reg = grammars.NewBuiltin()

func analyze(t *testing.T, path, src string) *FileAnalysis {
	t.Helper()
	lang := reg.ForPath(path)
	require.NotNil(t, lang, "no grammar for %s", path)
	a, err := Analyze(lang, []byte(src))
	require.NoError(t, err)
	return a
}

func symbols(a *FileAnalysis) []string {
	var out []string
	for _, d := range a.Definitions {
		out = append(out, d.Symbol)
	}
	return out
}

func def(t *testing.T, a *FileAnalysis, sym string) Definition {
	t.Helper()
	for _, d := range a.Definitions {
		if d.Symbol == sym {
			return d
		}
	}
	t.Fatalf("no definition %q in %v", sym, symbols(a))
	return Definition{}
}

const goSrc = `package orders

import (
	"os"
	pay "example.com/payments"
)

// Order is a customer order.
type Order struct{ ID string }

type (
	Status int
	Amount int64
)

// Refund returns money.
func (o *Order) Refund(amount Amount) error {
	key := os.Getenv("PAYMENTS_KEY")
	r.Post("/orders/refund", handler)
	return pay.Charge(key, amount)
}

func helper() int { return 1 }

var DefaultTimeout = 30
`

func TestAnalyze_Go(t *testing.T) {
	a := analyze(t, "orders/order.go", goSrc)
	assert.Equal(t, []string{"Order", "Status", "Amount", "Order.Refund", "helper"}, symbols(a))
	refund := def(t, a, "Order.Refund")
	assert.Equal(t, "method", refund.Kind)
	assert.Equal(t, "func (o *Order) Refund(amount Amount) error", refund.Signature)
	assert.True(t, strings.HasPrefix(refund.Content, "// Refund returns money."), "doc comment travels with the chunk")
	assert.Equal(t, []EnvRead{{Name: "PAYMENTS_KEY", Line: 18}}, refund.EnvReads)
	var callees []string
	for _, c := range refund.Calls {
		callees = append(callees, c.Callee)
	}
	assert.Equal(t, []string{"os.Getenv", "r.Post", "pay.Charge"}, callees)
	assert.Equal(t, []string{"/orders/refund"}, refund.Calls[1].StringArgs)
	assert.Equal(t, 16, refund.StartLine)
	require.Len(t, a.Imports, 2)
	assert.Equal(t, Import{Path: "example.com/payments", Alias: "pay", Line: 5, Text: `pay"example.com/payments"`}, a.Imports[1])
	require.NotNil(t, a.Module)
	assert.Contains(t, a.Module.Content, "DefaultTimeout")
	assert.False(t, a.HasErrors)
}

func TestAnalyze_Java_QualifiesAndDisambiguatesOverloads(t *testing.T) {
	a := analyze(t, "A.java", `package a;
import a.pay.Gateway;
@RestController
@RequestMapping("/api")
public class Api {
  @GetMapping("/orders/{id}")
  public Order get(String id) { return Gateway.find(System.getenv("REGION")); }
  public Order get(long id) { return null; }
  class Inner { void x() {} }
}`)
	assert.Equal(t, []string{"Api", "Api.get(String id)", "Api.get(long id)", "Api.Inner", "Api.Inner.x"}, symbols(a))
	api := def(t, a, "Api")
	require.Len(t, api.Annotations, 2)
	assert.Equal(t, "RequestMapping", api.Annotations[1].Name)
	assert.Equal(t, []string{"/api"}, api.Annotations[1].StringArgs)
	assert.Contains(t, api.Content, "public Order get(String id) …", "class chunk is an outline")
	assert.NotContains(t, api.Content, "Gateway.find", "method bodies are not duplicated into the class chunk")
	get := def(t, a, "Api.get(String id)")
	assert.Equal(t, "GetMapping", get.Annotations[0].Name)
	assert.Equal(t, []EnvRead{{Name: "REGION", Line: 7}}, get.EnvReads)
	assert.Equal(t, "a.pay.Gateway", a.Imports[0].Path)
	assert.Equal(t, []string{"Gateway"}, a.Imports[0].Names)
}

func TestAnalyze_Python_DecoratorsDocstringsModule(t *testing.T) {
	a := analyze(t, "svc/api.py", `import os
from svc.db import Session, engine

@router.post("/users")
def create_user(payload: dict) -> dict:
    """Create a user."""
    return Session(os.environ["DB_URL"]).add(payload)

class Repo(Base):
    def find(self, id):
        return os.getenv("CACHE_TTL")

app = FastAPI()
`)
	assert.Equal(t, []string{"create_user", "Repo", "Repo.find"}, symbols(a))
	cu := def(t, a, "create_user")
	assert.True(t, strings.HasPrefix(cu.Content, `@router.post("/users")`))
	assert.Equal(t, "router.post", cu.Annotations[0].Name)
	assert.Equal(t, []string{"/users"}, cu.Annotations[0].StringArgs)
	assert.Equal(t, []EnvRead{{Name: "DB_URL", Line: 7}}, cu.EnvReads)
	assert.Equal(t, []EnvRead{{Name: "CACHE_TTL", Line: 11}}, def(t, a, "Repo.find").EnvReads)
	assert.Equal(t, "svc.db", a.Imports[1].Path)
	assert.Equal(t, []string{"Session", "engine"}, a.Imports[1].Names)
	require.NotNil(t, a.Module)
	assert.Contains(t, a.Module.Content, "FastAPI()")
}

func TestAnalyze_TypeScript_ArrowFunctionsDecoratorsModuleCalls(t *testing.T) {
	a := analyze(t, "src/app.ts", `import { Router } from 'express';
import cfg from '../config';
export class OrdersController {
  @Get('orders')
  list(): Order[] { return this.svc.all(process.env.PAGE_SIZE); }
}
export const handler = async (req: Req) => { return cfg.get(process.env["API_KEY"]); };
router.get('/health', handler);
`)
	assert.Equal(t, []string{"OrdersController", "OrdersController.list", "handler"}, symbols(a))
	list := def(t, a, "OrdersController.list")
	require.Len(t, list.Annotations, 1)
	assert.Equal(t, []string{"orders"}, list.Annotations[0].StringArgs)
	assert.Equal(t, []EnvRead{{Name: "PAGE_SIZE", Line: 5}}, list.EnvReads)
	assert.Equal(t, []EnvRead{{Name: "API_KEY", Line: 7}}, def(t, a, "handler").EnvReads)
	assert.True(t, strings.HasPrefix(def(t, a, "handler").Content, "export const handler"))
	require.NotNil(t, a.Module)
	assert.Equal(t, "router.get", a.Module.Calls[0].Callee)
	assert.Equal(t, []string{"/health"}, a.Module.Calls[0].StringArgs)
	assert.Equal(t, "../config", a.Imports[1].Path)
	assert.Equal(t, []string{"cfg"}, a.Imports[1].Names)
}

func TestAnalyze_TSXAndJavaScript(t *testing.T) {
	a := analyze(t, "ui/Button.tsx", "export function Button(p: Props) { return <button onClick={p.on}>{p.label}</button>; }\n")
	assert.Equal(t, []string{"Button"}, symbols(a))
	a = analyze(t, "srv.js", "const express = require('express');\nfunction start() { app.listen(process.env.PORT); }\nmodule.exports = { start };\n")
	assert.Equal(t, []string{"start"}, symbols(a))
	assert.Equal(t, []EnvRead{{Name: "PORT", Line: 2}}, def(t, a, "start").EnvReads)
}

func TestAnalyze_Rust_ImplsTraitsAttributes(t *testing.T) {
	a := analyze(t, "src/lib.rs", `use crate::db::Pool;
#[get("/items")]
pub async fn items(pool: Pool) -> Json<Vec<Item>> { let k = std::env::var("DB").unwrap(); todo!() }
pub struct Item { id: u64 }
impl Display for Item { fn fmt(&self, f: &mut Formatter) -> Result { Ok(()) } }
impl Item { pub fn new() -> Self { Item { id: 0 } } }
mod util { pub fn helper() {} }
`)
	assert.Equal(t, []string{"items", "Item", "impl Display for Item", "Item.fmt", "impl Item", "Item.new", "util", "util.helper"}, symbols(a))
	items := def(t, a, "items")
	assert.Equal(t, "get", items.Annotations[0].Name)
	assert.Equal(t, []EnvRead{{Name: "DB", Line: 3}}, items.EnvReads)
	assert.Nil(t, a.Module, "a leading attribute belongs to its function, not to the module")
	assert.Equal(t, "crate::db::Pool", a.Imports[0].Path)
}

func TestAnalyze_ParseErrorsAreFlagged(t *testing.T) {
	a := analyze(t, "bad.go", "package p\nfunc (\n")
	assert.True(t, a.HasErrors)
}

func TestCode_ChunkIDsAreStableAndScoped(t *testing.T) {
	a := analyze(t, "orders/order.go", goSrc)
	c1 := Code("acme/shop", "repo-1", "orders/order.go", a)
	c2 := Code("acme/shop", "repo-1", "orders/order.go", analyze(t, "orders/order.go", goSrc))
	require.Equal(t, len(c1), len(c2))
	for i := range c1 {
		assert.Equal(t, c1[i].ID, c2[i].ID)
		assert.Len(t, c1[i].ID, 16)
		assert.Equal(t, ports.SourceCode, c1[i].Source)
		assert.Equal(t, Hash(c1[i].Content), c1[i].ContentHash)
	}
	other := Code("acme/other", "repo-2", "orders/order.go", a)
	assert.NotEqual(t, c1[0].ID, other[0].ID, "scope is part of the ID")

	edited := strings.Replace(goSrc, "return pay.Charge(key, amount)", "return pay.Charge(key, amount*2)", 1)
	c3 := Code("acme/shop", "repo-1", "orders/order.go", analyze(t, "orders/order.go", edited))
	byID := map[string]ports.Chunk{}
	for _, c := range c1 {
		byID[c.ID] = c
	}
	changed := 0
	for _, c := range c3 {
		old, ok := byID[c.ID]
		require.True(t, ok, "editing a body keeps every ID")
		if old.ContentHash != c.ContentHash {
			changed++
			assert.Equal(t, "Order.Refund", c.Symbol)
		}
	}
	assert.Equal(t, 1, changed)
}

func TestFile_FallsBackAndSkipsBinary(t *testing.T) {
	cs, a, err := File(reg, "s", "r", "deploy/values.yaml", []byte("replicas: 2\n"))
	require.NoError(t, err)
	assert.Nil(t, a)
	require.Len(t, cs, 1)
	assert.Equal(t, "text", cs[0].Language)
	assert.Equal(t, "deploy/values.yaml", cs[0].Symbol)

	cs, _, err = File(reg, "s", "r", "logo.png", []byte{0x89, 'P', 'N', 'G', 0, 0, 1})
	require.NoError(t, err)
	assert.Empty(t, cs)

	cs, a, err = File(reg, "s", "r", "x.go", []byte(goSrc))
	require.NoError(t, err)
	assert.NotNil(t, a)
	assert.Len(t, cs, 6)

	assert.Empty(t, PlainText("s", "r", "empty.txt", []byte("  \n")))
}

func TestDocs_SectionsByH2H3_IgnoresFencedHeadings(t *testing.T) {
	md := "# Title\nIntro text.\n\n## Setup\nInstall it.\n```sh\n## not a heading\n```\n### Linux\nApt.\n## Setup\nSecond setup section.\n<!-- dth:chunk 0123456789abcdef -->\n## Usage!\nRun it.\n"
	cs := Docs("acme/shop", "r", "docs/generated/a.go.md", []byte(md), DocOptions{})
	var syms []string
	for _, c := range cs {
		syms = append(syms, c.Symbol)
		assert.Equal(t, "markdown", c.Language)
		assert.Equal(t, ports.SourceGeneratedDoc, c.Source)
		assert.NotContains(t, c.Content, "dth:chunk", "markers are not indexed")
	}
	assert.Equal(t, []string{"__intro__", "setup", "setup/linux", "setup#2", "usage"}, syms)
	assert.Contains(t, cs[1].Content, "## not a heading", "headings inside code fences are content")
}

func TestDocs_LongSectionsSplitWithOverlap(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Big\n")
	for i := 0; i < 60; i++ {
		b.WriteString(strings.Repeat("word ", 20))
		b.WriteString("\n\n")
	}
	cs := Docs("s", "r", "big.md", []byte(b.String()), DocOptions{MaxTokens: 200, OverlapTokens: 20, Source: ports.SourceImportedDoc})
	require.Greater(t, len(cs), 3)
	assert.Equal(t, "big", cs[0].Symbol)
	assert.Equal(t, "big#part2", cs[1].Symbol)
	for _, c := range cs {
		assert.LessOrEqual(t, EstimateTokens(c.Content), 260, "parts respect the budget (plus overlap)")
		assert.Equal(t, ports.SourceImportedDoc, c.Source)
	}
	one := splitByTokens(strings.Repeat("x", 5000), 200, 20)
	assert.Greater(t, len(one), 1, "a single huge paragraph is still split")
}

func TestSlug(t *testing.T) {
	assert.Equal(t, "getting-started-v2", Slug("Getting Started (v2)!"))
	assert.Equal(t, "api", Slug("  API  "))
	assert.Equal(t, "", Slug("!!!"))
}

func TestRenameRekey_MapsEveryChunkToNewPath(t *testing.T) {
	a := analyze(t, "old/order.go", goSrc)
	old := Code("acme/shop", "r", "old/order.go", a)
	txt := PlainText("acme/shop", "r", "old/order.go", []byte("x"))
	m := RenameRekey("acme/shop", "old/order.go", "new/order.go", append(old, txt...))
	assert.Len(t, m, len(old)+1)
	for oldID, nc := range m {
		assert.NotEqual(t, oldID, nc.ID)
		assert.Equal(t, "new/order.go", nc.Path)
		assert.Equal(t, ID("acme/shop", "new/order.go", nc.Symbol), nc.ID)
	}
	assert.Equal(t, "new/order.go", m[txt[0].ID].Symbol)
}

func TestHelpers(t *testing.T) {
	assert.True(t, IsMarkdown("README.MD"))
	assert.False(t, IsMarkdown("a.go"))
	assert.Equal(t, "x", unquote(`"x"`))
	assert.Equal(t, "doc", unquote(`"""doc"""`))
	assert.Equal(t, "raw", unquote(`r#"raw"#`))
	assert.Equal(t, "tpl", unquote("`tpl`"))
	assert.Equal(t, "Get", lastSegment("a::b.Get"))
	assert.Equal(t, "GetMapping", annotationName(`@GetMapping("/x")`))
	assert.Equal(t, "get", annotationName(`#[get("/x")]`))
}
