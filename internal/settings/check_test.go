package settings

import (
	"context"
	"strings"
	"testing"
)

func checkSrc(t *testing.T, yml string, opts CheckOptions) CheckReport {
	t.Helper()
	rep, err := Check(context.Background(), []Source{{Name: "s.yaml", Data: []byte(yml)}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestCheckOwnersDomainsAndReferences(t *testing.T) {
	yml := `
auth:
  password: false
  sso: { issuer: https://idp.example, client_id: x, client_secret: "${env:DTH_SECRET_OIDC}", allowed_domains: [acme.com] }
users:
  - { email: Lead@Acme.com, role: owner }
  - { email: contractor@other.io, role: admin }
  - { email: reader@acme.com }
providers:
  - { name: a, kind: anthropic, api_key: "${awssm:prod/dth#key}" }
  - { name: b, kind: anthropic, api_key: "${awssm:prod/dth#key}", base_url: "$${literal}" } # ${env:IN_COMMENT}
`
	rep := checkSrc(t, yml, CheckOptions{RequireOwner: true})
	if strings.Join(rep.Owners, ",") != "lead@acme.com" || strings.Join(rep.Admins, ",") != "contractor@other.io" {
		t.Fatalf("owners %v admins %v", rep.Owners, rep.Admins)
	}
	if !rep.SSO || rep.Password == nil || *rep.Password || rep.Resolved {
		t.Fatalf("report %+v", rep)
	}
	if strings.Join(rep.References, " ") != "${awssm:prod/dth#key} ${env:DTH_SECRET_OIDC}" {
		t.Fatalf("references %v", rep.References)
	}
	if len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "contractor@other.io") {
		t.Fatalf("problems %v", rep.Problems)
	}
}

func TestCheckRequireOwner(t *testing.T) {
	yml := "users:\n  - { email: a@acme.com, role: admin }\n  - { email: b@acme.com, role: owner, disabled: true }\n"
	if rep := checkSrc(t, yml, CheckOptions{}); !rep.OK() {
		t.Fatalf("owner not required: %v", rep.Problems)
	}
	rep := checkSrc(t, yml, CheckOptions{RequireOwner: true})
	if rep.OK() || !strings.Contains(rep.Problems[0], "no owner") {
		t.Fatalf("problems %v", rep.Problems)
	}
}

func TestCheckResolveReportsMissingSecretsWithoutValues(t *testing.T) {
	yml := `providers:
  - { name: a, kind: anthropic, api_key: "${env:DTH_SECRET_SET}" }
  - { name: b, kind: openai, api_key: "${env:DTH_SECRET_MISSING}" }
`
	env := map[string]string{"DTH_SECRET_SET": "sk-super-secret"}
	r := &Resolver{Getenv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }}
	rep := checkSrc(t, yml, CheckOptions{Resolver: r})
	if !rep.Resolved || len(rep.Problems) != 1 || !strings.Contains(rep.Problems[0], "DTH_SECRET_MISSING") {
		t.Fatalf("problems %v", rep.Problems)
	}
	for _, p := range rep.Problems {
		if strings.Contains(p, "sk-super-secret") {
			t.Fatal("a secret value leaked into the report")
		}
	}
}

func TestCheckParseErrors(t *testing.T) {
	if _, err := Check(context.Background(), []Source{{Name: "s.yaml", Data: []byte("userz: []\n")}}, CheckOptions{}); err == nil {
		t.Fatal("unknown key accepted")
	}
}
