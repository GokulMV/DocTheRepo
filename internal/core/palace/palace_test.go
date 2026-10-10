package palace

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
)

var reg = grammars.NewBuiltin()

func extract(t *testing.T, p, src string) Graph {
	t.Helper()
	a, err := chunker.Analyze(reg.ForPath(p), []byte(src))
	require.NoError(t, err)
	return ExtractFile("acme/shop", p, "c1", a, nil)
}

func edgeSet(g Graph, kind string) []string {
	var out []string
	for _, e := range g.EdgesOf(kind) {
		out = append(out, e.Src.Key+" -> "+e.Dst.Key)
	}
	sort.Strings(out)
	return out
}

func dsts(g Graph, kind string) []string {
	var out []string
	for _, e := range g.EdgesOf(kind) {
		out = append(out, e.Dst.Key)
	}
	sort.Strings(out)
	return out
}

func TestExtractFile_GoStructureCallsEnvRoutesTopics(t *testing.T) {
	g := extract(t, "svc/orders/api.go", `package orders

func Routes(r chi.Router) {
	r.Get("/orders/{id}", getOrder)
	r.Post("/orders", create)
	http.HandleFunc("DELETE /orders/{id}", del)
	cache.Get("not-a-route")
}

func getOrder(w http.ResponseWriter, r *http.Request) {
	db, _ := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	_ = load(db)
	producer.Publish("orders.viewed", evt)
	res.Send("ok")
}

func load(db *sql.DB) error { return nil }

func consume() { reader.Subscribe("payments.settled") }
`)
	assert.Equal(t, []string{
		"acme/shop -> acme/shop:svc",
		"acme/shop:svc -> acme/shop:svc/orders",
		"acme/shop:svc/orders -> acme/shop:svc/orders/api.go",
		"acme/shop:svc/orders/api.go -> acme/shop:svc/orders/api.go#Routes",
		"acme/shop:svc/orders/api.go -> acme/shop:svc/orders/api.go#consume",
		"acme/shop:svc/orders/api.go -> acme/shop:svc/orders/api.go#getOrder",
		"acme/shop:svc/orders/api.go -> acme/shop:svc/orders/api.go#load",
	}, edgeSet(g, EdgeContains))
	assert.Equal(t, []string{"acme/shop:svc/orders/api.go#getOrder -> acme/shop:svc/orders/api.go#load"}, edgeSet(g, EdgeCalls),
		"passing getOrder as a handler is not a call; calling load is")
	assert.Equal(t, []string{"acme/shop:DELETE /orders/{id}", "acme/shop:GET /orders/{id}", "acme/shop:POST /orders"}, dsts(g, EdgeExposes))
	assert.Equal(t, []string{"DATABASE_URL"}, dsts(g, EdgeReadsEnv))
	assert.Equal(t, []string{"orders.viewed"}, dsts(g, EdgePublishes), "res.Send(\"ok\") is not a topic")
	assert.Equal(t, []string{"payments.settled"}, dsts(g, EdgeSubscribes))
	assert.Equal(t, []string{"sql_database:DATABASE_URL", "sql_database:acme/shop"}, dsts(g, EdgeUsesDatastore))
	for _, e := range g.EdgesOf(EdgeExposes) {
		assert.Equal(t, "svc/orders/api.go", e.Evidence.Path)
		assert.Equal(t, "c1", e.Evidence.Commit)
		assert.NotZero(t, e.Evidence.Line)
	}
	ep, ok := g.Find(EndpointRef("acme/shop", "POST", "/orders"))
	require.True(t, ok)
	assert.Equal(t, "POST /orders", ep.Name)
}

func TestExtractFile_SpringPrefixesListenersAndJaxRS(t *testing.T) {
	g := extract(t, "src/main/java/com/acme/OrderApi.java", `package com.acme;
@RestController
@RequestMapping("/api/v1")
public class OrderApi {
  @GetMapping("/orders/{id}")
  public Order get(String id) { return repo.find(System.getenv("REGION")); }
  @PostMapping
  public Order create(Order o) { kafkaTemplate.send("orders.created", o); return o; }
  @KafkaListener(topics = "payments.settled")
  public void onPaid(Event e) {}
}`)
	assert.Equal(t, []string{"acme/shop:GET /api/v1/orders/{id}", "acme/shop:POST /api/v1"}, dsts(g, EdgeExposes))
	assert.Equal(t, []string{"orders.created"}, dsts(g, EdgePublishes))
	assert.Equal(t, []string{"payments.settled"}, dsts(g, EdgeSubscribes))
	assert.Equal(t, []string{"REGION"}, dsts(g, EdgeReadsEnv))

	jax := extract(t, "R.java", `@Path("/users")
public class R {
  @GET
  @Path("/{id}")
  public User get() { return null; }
}`)
	assert.Equal(t, []string{"acme/shop:GET /users/{id}"}, dsts(jax, EdgeExposes))
}

func TestExtractFile_PythonFastAPIAndBoto(t *testing.T) {
	g := extract(t, "app/api.py", `import boto3
@router.get("/items/{id}")
def get_item(id):
    table = boto3.resource("dynamodb")
    return os.environ["REDIS_URL"]

@cache.get("key")
def cached():
    pass

@app.route("/health")
def health():
    return "ok"
`)
	assert.Equal(t, []string{"acme/shop:ANY /health", "acme/shop:GET /items/{id}"}, dsts(g, EdgeExposes), "@cache.get(\"key\") is not a route")
	assert.Equal(t, []string{"dynamodb:acme/shop", "redis:REDIS_URL"}, dsts(g, EdgeUsesDatastore))
}

func TestExtractFile_TypeScriptExpressAndNest(t *testing.T) {
	g := extract(t, "src/server.ts", `
@Controller('users')
export class UsersController {
  @Get(':id')
  find(id: string) { return this.svc.find(id); }
  @EventPattern('user.deleted')
  onDeleted() {}
}
app.get('/health', health);
app.post('/login', login);
`)
	assert.Equal(t, []string{"acme/shop:GET /health", "acme/shop:GET /users/:id", "acme/shop:POST /login"}, dsts(g, EdgeExposes))
	assert.Equal(t, []string{"user.deleted"}, dsts(g, EdgeSubscribes))
	var routeFromFile bool
	for _, e := range g.EdgesOf(EdgeExposes) {
		if e.Src == FileRef("acme/shop", "src/server.ts") {
			routeFromFile = true
		}
	}
	assert.True(t, routeFromFile, "module-level route registration is attributed to the file")
}

func TestExtractFile_RustActixAndAxum(t *testing.T) {
	g := extract(t, "src/main.rs", `
#[get("/items")]
async fn items() -> impl Responder { let _ = std::env::var("PG_URL"); }
fn app() -> Router { Router::new().route("/health", get(health)) }
`)
	assert.Equal(t, []string{"acme/shop:ANY /health", "acme/shop:GET /items"}, dsts(g, EdgeExposes))
	assert.Equal(t, []string{"postgres:PG_URL"}, dsts(g, EdgeUsesDatastore))
}

func TestExtractFile_ResolverAddsCrossFileCalls(t *testing.T) {
	a, err := chunker.Analyze(reg.ForPath("a.go"), []byte("package p\nfunc F() { pay.Charge(1) }\n"))
	require.NoError(t, err)
	g := ExtractFile("acme/shop", "a.go", "c", a, func(callee string) (string, string, bool) {
		if callee == "pay.Charge" {
			return "payments/pay.go", "Charge", true
		}
		return "", "", false
	})
	assert.Equal(t, []string{"acme/shop:a.go#F -> acme/shop:payments/pay.go#Charge"}, edgeSet(g, EdgeCalls))
	assert.Equal(t, []string{"acme/shop -> acme/shop:a.go"}, edgeSet(g, EdgeContains)[:1], "root files hang off the repo")
}

func TestExtractImports_GoPackagesOfTheSameModule(t *testing.T) {
	// internal/api calls a method on a field typed by internal/mcpconn: no call edge can be resolved
	// without types, but the import names the package exactly.
	src := `package api

import (
	"context"

	"github.com/stretchr/testify/assert"
	conn "acme.dev/shop/internal/mcpconn"
	"acme.dev/shop/internal/api/views"
	"acme.dev/shop/internal/api"
)

type Server struct{ client *conn.Client }

func (s *Server) Do(ctx context.Context) { s.client.Call(ctx); assert.True(nil, true) }
`
	a, err := chunker.Analyze(reg.ForPath("internal/api/server.go"), []byte(src))
	require.NoError(t, err)
	assert.Empty(t, edgeSet(ExtractFile("acme/shop", "internal/api/server.go", "c", a, nil), EdgeCalls), "the method call stays unresolved")

	g := ExtractImports("acme/shop", "internal/api/server.go", "c", a, "acme.dev/shop")
	assert.Equal(t, []string{
		"acme/shop:internal/api/server.go -> acme/shop:internal/api/views",
		"acme/shop:internal/api/server.go -> acme/shop:internal/mcpconn",
	}, edgeSet(g, EdgeImports), "stdlib, external and same-package imports are left out")
	for _, e := range g.EdgesOf(EdgeImports) {
		assert.Equal(t, KindModule, e.Dst.Kind)
		assert.Equal(t, "internal/api/server.go", e.Evidence.Path)
		assert.Positive(t, e.Evidence.Line)
	}

	assert.Empty(t, ExtractImports("acme/shop", "internal/api/server.go", "c", a, "").Edges, "no go.mod, no edges")
	ts, err := chunker.Analyze(reg.ForPath("web/a.ts"), []byte("import { x } from './b'\nexport const y = x\n"))
	require.NoError(t, err)
	assert.Empty(t, ExtractImports("acme/shop", "web/a.ts", "c", ts, "acme.dev/shop").Edges, "Go only")
}

func TestExtractManifest(t *testing.T) {
	g := ExtractManifest("acme/shop", "web/package.json", "c", []byte(`{"dependencies":{"react":"^18"},"devDependencies":{"jest":"29"}}`))
	assert.Equal(t, []string{"npm:jest", "npm:react"}, dsts(g, EdgeDependsOn))
	g = ExtractManifest("acme/shop", "pom.xml", "c", []byte(`<project><dependencies><dependency><groupId>org.x</groupId><artifactId>y</artifactId><version>1</version></dependency></dependencies></project>`))
	assert.Equal(t, []string{"maven:org.x:y"}, dsts(g, EdgeDependsOn))
	g = ExtractManifest("acme/shop", "pyproject.toml", "c", []byte("[tool.poetry.dependencies]\npython = \"^3.11\"\nfastapi = \"^0.110\"\n"))
	assert.Equal(t, []string{"pypi:fastapi"}, dsts(g, EdgeDependsOn), "the python interpreter is not a dependency")
	assert.Empty(t, ExtractManifest("acme/shop", "package.json", "c", []byte("{")).Edges)
	assert.Equal(t, "go", ecosystem("go.mod"))
	assert.Equal(t, "cargo", ecosystem("Cargo.toml"))
}

func TestExtractCodeowners(t *testing.T) {
	g := ExtractCodeowners("acme/shop", ".github/CODEOWNERS", "c", []byte(`# comment
*       @acme/platform
/payments/   @acme/payments alice@acme.com # trailing comment
*.md    @docs-team
[Section]
/web/** @bob
`))
	assert.Equal(t, []string{
		"acme/shop -> @acme/platform",
		"acme/shop:payments -> @acme/payments",
		"acme/shop:payments -> alice@acme.com",
		"acme/shop:web -> @bob",
	}, edgeSet(g, EdgeOwnedBy))
	team, ok := g.Find(Ref{KindTeam, "@acme/payments"})
	require.True(t, ok)
	assert.Equal(t, "@acme/payments", team.Name)
	assert.True(t, g.Has(Ref{KindPerson, "@bob"}))
	assert.True(t, IsCodeowners(".github/CODEOWNERS"))
	assert.False(t, IsCodeowners("CODEOWNERS.md"))
}

func TestExtractDeployments(t *testing.T) {
	cases := map[string]struct {
		content string
		want    []string
	}{
		"deploy/helm/Chart.yaml": {"apiVersion: v2\nname: orders-api\nversion: 1.0.0\n", []string{"orders-api"}},
		"serverless.yml":         {"service: billing\nprovider:\n  name: aws\n", []string{"billing"}},
		"docker-compose.yml":     {"version: '3'\nservices:\n  api:\n    image: x\n  worker:\n    image: y\nvolumes:\n  data:\n", []string{"api", "worker"}},
		"Dockerfile":             {"FROM golang\nLABEL org.opencontainers.image.title=\"inventory\"\n", []string{"inventory"}},
		"k8s/deploy.yaml":        {"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: checkout\n  labels:\n    a: b\n---\nkind: Service\nmetadata:\n  name: checkout-svc\n", []string{"checkout"}},
		"deploy/helm/t/dep.yaml": {"kind: Deployment\nmetadata:\n  name: {{ .Release.Name }}\n", nil},
	}
	for p, c := range cases {
		assert.True(t, IsDeploymentFile(p), p)
		g := ExtractDeployments("acme/shop", p, "c", []byte(c.content))
		assert.Equal(t, c.want, dsts(g, EdgeDeployedAs), p)
	}
	assert.False(t, IsDeploymentFile("src/config.yaml"))
}

func TestGraph_DedupeMergeAndDiff(t *testing.T) {
	var a Graph
	r := a.AddEntity(Entity{Ref: EnvVarRef("X"), Name: "X", Attrs: map[string]string{"a": "1"}})
	a.AddEntity(Entity{Ref: EnvVarRef("X"), Name: "other", Attrs: map[string]string{"b": "2"}})
	a.AddEdge(RepoRef("r"), EdgeReadsEnv, r, Evidence{Line: 1})
	a.AddEdge(RepoRef("r"), EdgeReadsEnv, r, Evidence{Line: 2})
	require.Len(t, a.Entities, 1)
	assert.Equal(t, "X", a.Entities[0].Name)
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, a.Entities[0].Attrs)
	require.Len(t, a.Edges, 1)
	assert.Equal(t, 1, a.Edges[0].Evidence.Line)

	var b Graph
	b.AddEntity(Entity{Ref: EnvVarRef("Y"), Name: "Y"})
	b.AddEdge(RepoRef("r"), EdgeReadsEnv, EnvVarRef("Y"), Evidence{})
	d := Diff(a, b)
	assert.Equal(t, []Ref{EnvVarRef("X")}, d.RemovedEntities)
	assert.Equal(t, "Y", d.AddedEntities[0].Name)
	assert.Len(t, d.AddedEdges, 1)
	assert.Len(t, d.RemovedEdges, 1)

	a.Merge(b)
	assert.Len(t, a.Entities, 2)
	assert.Len(t, a.Edges, 2)
	_, ok := a.Find(Ref{KindTeam, "nope"})
	assert.False(t, ok)
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, "/api/x", joinRoute("api/", "/x"))
	assert.Equal(t, "/api", joinRoute("/api", ""))
	assert.Equal(t, "/", joinRoute("", ""))
	assert.Equal(t, "/x", joinRoute("", "x"))
	assert.True(t, validTopic("orders.created"))
	assert.False(t, validTopic("/path"))
	assert.False(t, validTopic("http://x"))
	assert.False(t, validTopic("has space"))
	assert.Equal(t, "Svc", parentOf("Svc.get(String id)"))
	assert.Equal(t, "", parentOf("f"))
}
