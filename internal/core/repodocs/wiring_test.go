package repodocs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func wiresOf(ws []Wire, kind string) map[string]int {
	m := map[string]int{}
	for _, w := range ws {
		if w.Kind == kind {
			m[w.Value] = w.Line
		}
	}
	return m
}

func TestExtractWiringFromConfigAndCI(t *testing.T) {
	k8s := `apiVersion: v1
kind: Service
metadata:
  name: billing
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: billing
spec:
  rules:
    - host: billing.acme.io
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: app
          image: ghcr.io/acme/billing:1.4.2
          env:
            - name: CATALOG_URL
              value: http://catalog.shop.svc.cluster.local:8080/v1
`
	ws := ExtractWiring("acme/billing", "deploy/k8s.yaml", k8s)
	assert.Equal(t, map[string]int{"billing": 4, "billing.acme.io": 12}, wiresOf(ws, "host"))
	assert.Equal(t, 21, wiresOf(ws, "image_use")["ghcr.io/acme/billing"])
	assert.Equal(t, 24, wiresOf(ws, "calls")["catalog.shop.svc.cluster.local"])

	env := "# where things are\nPAYMENTS_URL=http://payments:8080\nLEDGER_HOST=ledger.internal:9090\nPORT=8080\nSTRIPE=https://api.stripe.com/v1\nDEBUG=true\n"
	calls := wiresOf(ExtractWiring("acme/web", ".env.example", env), "calls")
	assert.Equal(t, map[string]int{"payments": 2, "ledger.internal": 3, "api.stripe.com": 5}, calls)

	wf := `name: ci
on: push
jobs:
  build:
    uses: acme/platform/.github/workflows/go.yml@main
  image:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/checkout@v4
        with:
          repository: acme/protos
      - uses: docker/build-push-action@v6
        with:
          tags: |
            ghcr.io/${{ github.repository }}:latest
            ghcr.io/acme/web:${{ github.sha }}
      - run: gh workflow run deploy.yml --repo acme/deployer
`
	ws = ExtractWiring("Acme/Web", ".github/workflows/ci.yml", wf)
	refs := wiresOf(ws, "ci_ref")
	for _, want := range []string{"acme/platform", "acme/protos", "acme/deployer"} {
		assert.Contains(t, refs, want)
	}
	assert.NotContains(t, refs, "actions/checkout")
	assert.NotContains(t, refs, "acme/web", "its own name is not a reference")
	assert.Contains(t, wiresOf(ws, "image_pub"), "ghcr.io/acme/web")

	df := "FROM golang:1.26 AS build\nFROM ghcr.io/acme/base-image:2 \nFROM scratch\n"
	assert.Equal(t, map[string]int{"golang": 1, "ghcr.io/acme/base-image": 2}, wiresOf(ExtractWiring("acme/web", "Dockerfile", df), "image_use"))

	gl := "deploy:\n  trigger:\n    project: acme/deployer\n    branch: main\n"
	assert.Contains(t, wiresOf(ExtractWiring("acme/web", ".gitlab-ci.yml", gl), "ci_ref"), "acme/deployer")
}

func TestWiringFile(t *testing.T) {
	for _, p := range []string{".github/workflows/ci.yml", "deploy/k8s.yaml", "charts/web/values.yaml", ".env.example", "config/app.toml",
		"src/main/resources/application.properties", "docker-compose.yml", "Dockerfile", "infra/main.tf", "fly.toml", ".gitlab-ci.yml"} {
		assert.True(t, WiringFile(p), p)
	}
	for _, p := range []string{"src/app.go", "package.json", "README.md", "web/src/data.json", "go.sum"} {
		assert.False(t, WiringFile(p), p)
	}
}

func TestMatchWiring(t *testing.T) {
	repos := []WiringRepo{
		{ID: "w", Name: "acme/web", Wires: []Wire{
			{Kind: "calls", Value: "payments", Path: ".env", Line: 2},                   // Compose / same-namespace name
			{Kind: "calls", Value: "billing.acme.io", Path: "config/app.yaml", Line: 3}, // a host billing serves
			{Kind: "calls", Value: "api.stripe.com", Path: ".env", Line: 5},             // third party: no link
			{Kind: "calls", Value: "payments.example.org", Path: ".env", Line: 6},       // public name: no guess
			{Kind: "ci_ref", Value: "acme/platform", Note: "uses: acme/platform/x.yml@main", Path: ".github/workflows/ci.yml", Line: 5},
			{Kind: "image_use", Value: "ghcr.io/acme/base", Path: "Dockerfile", Line: 1},
		}},
		{ID: "p", Name: "acme/payments"},
		{ID: "b", Name: "acme/billing", Wires: []Wire{{Kind: "host", Value: "billing.acme.io", Path: "deploy/ingress.yaml", Line: 9}}},
		{ID: "pl", Name: "acme/platform", Wires: []Wire{{Kind: "image_pub", Value: "ghcr.io/acme/base", Path: ".github/workflows/base.yml", Line: 12}}},
	}
	got := map[string]string{}
	for _, l := range MatchWiring(repos) {
		got[l.FromName+">"+l.ToName+":"+l.Kind] = l.Via
	}
	assert.Equal(t, map[string]string{
		"acme/web>acme/payments:api":      "payments",
		"acme/web>acme/billing:api":       "billing.acme.io",
		"acme/web>acme/platform:pipeline": "uses: acme/platform/x.yml@main",
		"acme/web>acme/platform:image":    "ghcr.io/acme/base",
	}, got)
}
