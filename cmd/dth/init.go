package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GokulMV/DocTheRepo/internal/settings"
)

// target is where the Hub will run.
type target struct {
	id, label, defaultURL string
	prereqs               []string
	deploy                func(file string) []string
}

var targets = []target{
	{
		id: "laptop", label: "My computer, to try it (quickstart)", defaultURL: "http://localhost:8080",
		prereqs: []string{
			"macOS or Linux (Windows: WSL 2) with 4 GB of free memory and port 8080 free.",
			"Nothing else: ./scripts/quickstart.sh installs Docker, Go and Node.js if they are missing.",
		},
		deploy: func(f string) []string {
			return []string{"Export the secrets listed above in your shell, then:", "  ./scripts/quickstart.sh --settings " + f}
		},
	},
	{
		id: "compose", label: "A server with Docker Compose", defaultURL: "https://docs-hub.example.com",
		prereqs: []string{
			"A Linux server (2 vCPU, 4 GB RAM, 20 GB disk) with Docker and Docker Compose v2.",
			"A DNS name for the Hub and HTTPS in front of it (Caddy, nginx or a load balancer) forwarding to port 8080.",
			"Inbound HTTPS from your git host for push webhooks (without it the Hub polls every minute).",
		},
		deploy: func(f string) []string {
			return []string{
				"Copy deploy/compose/docker-compose.yml to the server, with this file in ./settings/ next to it.",
				"Put the secrets listed above in .env next to it, plus DTH_DB_PASSWORD, DTH_PUBLIC_URL and DTH_BIND=0.0.0.0.",
				"  docker compose up -d",
			}
		},
	},
	{
		id: "aws", label: "AWS (Terraform: ECS Fargate, RDS, ALB)", defaultURL: "https://docs-hub.example.com",
		prereqs: []string{
			"An AWS account and Terraform 1.6 or newer, with rights to create ECS, RDS, ALB, KMS, Secrets Manager and IAM roles.",
			"A VPC with two public and two private subnets (the private ones with outbound internet through NAT).",
			"A domain name and an ACM certificate for it (or a Route 53 zone to create the record in).",
			"The Hub image in a registry ECS can pull from (ghcr.io, or a mirror in ECR).",
		},
		deploy: func(f string) []string {
			return []string{
				"Fill in the secret values (they are stored in Secrets Manager, KMS-encrypted):",
				"  dth settings resolve -f " + f + " > hub.resolved.yaml",
				"In deploy/terraform/aws/terraform.tfvars set domain_name, acm_certificate_arn, vpc_id, the subnets,",
				"image, owner_email and auth_mode = \"local\", then:",
				"  terraform apply -var \"hub_settings=$(cat hub.resolved.yaml)\" && rm hub.resolved.yaml",
			}
		},
	},
	{
		id: "gcp", label: "Google Cloud (Terraform: Cloud Run, Cloud SQL)", defaultURL: "https://docs-hub.example.com",
		prereqs: []string{
			"A Google Cloud project with billing and Terraform 1.6 or newer, with rights to create Cloud Run, Cloud SQL, KMS, Secret Manager and a load balancer.",
			"A VPC network (the module can create private service access for Cloud SQL).",
			"A domain name you can point at the load balancer's IP (the certificate is Google-managed).",
			"The Hub image mirrored to Artifact Registry (Cloud Run cannot pull from ghcr.io).",
		},
		deploy: func(f string) []string {
			return []string{
				"Fill in the secret values (they are stored in Secret Manager):",
				"  dth settings resolve -f " + f + " > hub.resolved.yaml",
				"In deploy/terraform/gcp/terraform.tfvars set project_id, region, domain_name, image, owner_email and",
				"auth_mode = \"local\", then:",
				"  terraform apply -var \"hub_settings=$(cat hub.resolved.yaml)\" && rm hub.resolved.yaml",
				"Then point an A record for the domain at the load_balancer_ip output.",
			}
		},
	},
	{
		id: "kubernetes", label: "Kubernetes (Helm)", defaultURL: "https://docs-hub.example.com",
		prereqs: []string{
			"A Kubernetes cluster (1.27+) and Helm 3.",
			"PostgreSQL 16 with the pgvector extension, reachable from the cluster (RDS, Cloud SQL, or your own).",
			"An Ingress controller with TLS for the Hub's domain.",
			"A KMS key (AWS KMS with IRSA, or Cloud KMS with Workload Identity), or a shared master key Secret.",
		},
		deploy: func(f string) []string {
			return []string{
				"Store the file with its secret values as a Kubernetes Secret:",
				"  dth settings resolve -f " + f + " > hub.resolved.yaml",
				"  kubectl create secret generic dth-settings --from-file=hub.yaml=hub.resolved.yaml && rm hub.resolved.yaml",
				"  helm install dth deploy/helm/dth --set publicURL=<url> --set settings.existingSecret=dth-settings \\",
				"    --set auth.mode=local --set database.existingSecret=dth-db …   (see docs/install.md)",
			}
		},
	},
}

type ssoPreset struct {
	id, label, issuer, issuerHint string
	steps                         []string
}

var ssoPresets = []ssoPreset{
	{"google", "Google Workspace", "https://accounts.google.com", "", []string{
		"Google Cloud Console → APIs & Services → Credentials (https://console.cloud.google.com/apis/credentials).",
		"OAuth consent screen: choose “Internal”. Then Create credentials → OAuth client ID → Web application.",
		"Authorized redirect URI: the callback URL above. Copy the client ID and secret.",
	}},
	{"microsoft", "Microsoft Entra ID", "", "https://login.microsoftonline.com/<tenant-id>/v2.0", []string{
		"Entra admin center → App registrations → New registration; redirect URI (Web): the callback URL above.",
		"Certificates & secrets → New client secret (copy its Value). Token configuration → add optional claim “email” (ID token).",
		"Copy the Application (client) ID and the Directory (tenant) ID from Overview.",
	}},
	{"okta", "Okta", "", "https://<your-org>.okta.com", []string{
		"Okta Admin → Applications → Create App Integration → OIDC, Web Application.",
		"Sign-in redirect URI: the callback URL above. Assign the people or groups who may use it.",
		"Copy the client ID and secret from the General tab.",
	}},
	{"keycloak", "Keycloak", "", "https://<host>/realms/<realm>", []string{
		"Realm → Clients → Create client (OpenID Connect), turn on Client authentication.",
		"Valid redirect URIs: the callback URL above. Copy the secret from the Credentials tab.",
	}},
	{"other", "Other OpenID Connect provider", "", "the URL before /.well-known/openid-configuration", []string{
		"Create a web (confidential) OpenID Connect app with the callback URL above and the openid, email and profile scopes.",
	}},
}

type modelChoice struct {
	kind, label, keyEnv, model, fast string
	extras                           []string // extra settings asked for (key: question)
}

var modelChoices = []modelChoice{
	{kind: "anthropic", label: "Anthropic (Claude)", keyEnv: "DTH_SECRET_ANTHROPIC_API_KEY", model: "claude-sonnet-5-5", fast: "claude-haiku-4-5-20251001"},
	{kind: "openai", label: "OpenAI", keyEnv: "DTH_SECRET_OPENAI_API_KEY"},
	{kind: "azure_openai", label: "Azure OpenAI", keyEnv: "DTH_SECRET_AZURE_OPENAI_API_KEY", extras: []string{"base_url:Endpoint (https://<resource>.openai.azure.com)", "deployment:Chat deployment name"}},
	{kind: "bedrock", label: "AWS Bedrock (uses the Hub's AWS role, no key)", extras: []string{"region:AWS region"}},
	{kind: "vertex", label: "Google Vertex AI (uses the Hub's Google account, no key)", extras: []string{"project:Google Cloud project ID", "region:Region"}},
	{kind: "ollama", label: "Ollama on your network (no key)", extras: []string{"base_url:Ollama URL (http://ollama:11434/v1)"}},
}

// wizard asks questions on in and writes prompts to out.
type wizard struct {
	in  *bufio.Reader
	out io.Writer
}

func (w *wizard) ask(q, def string) string {
	if def != "" {
		fmt.Fprintf(w.out, "%s [%s]: ", q, def)
	} else {
		fmt.Fprintf(w.out, "%s: ", q)
	}
	line, _ := w.in.ReadString('\n')
	if line = strings.TrimSpace(line); line == "" {
		return def
	}
	return line
}

func (w *wizard) choose(q string, options []string, def int) int {
	fmt.Fprintln(w.out, q)
	for i, o := range options {
		fmt.Fprintf(w.out, "  %d) %s\n", i+1, o)
	}
	for {
		n, err := strconv.Atoi(w.ask("Choose", strconv.Itoa(def)))
		if err == nil && n >= 1 && n <= len(options) {
			fmt.Fprintln(w.out)
			return n - 1
		}
		fmt.Fprintf(w.out, "Enter a number from 1 to %d.\n", len(options))
	}
}

func (w *wizard) yes(q string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	a := strings.ToLower(w.ask(q+" ("+d+")", ""))
	if a == "" {
		return def
	}
	return strings.HasPrefix(a, "y")
}

func list(s string) []string {
	var out []string
	for _, x := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func envRef(name string) settings.Secret {
	return settings.Secret{Value: "${env:" + name + "}", Set: true}
}

func (a *app) initCmd() *cobra.Command {
	var outPath string
	var force bool
	c := &cobra.Command{
		Use:   "init",
		Short: "Answer a few questions before deploying; get a settings file and a checklist",
		Long: "init asks where the Hub will run, how people sign in (single sign-on or passwords), who owns it, which " +
			"model it uses and where your code lives. It writes a settings file the Hub applies when it starts, so a new " +
			"deployment comes up with sign-in, owners and models in place, and prints the prerequisites and deploy steps. " +
			"Secrets are never asked for: the file references environment variables you set where the Hub runs.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !force {
				if _, err := os.Stat(outPath); err == nil {
					return fmt.Errorf("%s exists; use --force to replace it, or -o for another file", outPath)
				}
			}
			doc, report, err := runWizard(&wizard{in: bufio.NewReader(cmd.InOrStdin()), out: a.out})
			if err != nil {
				return err
			}
			b, err := settings.Marshal(doc)
			if err != nil {
				return err
			}
			if err := os.WriteFile(outPath, b, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "\nWrote %s.\n\n%s", outPath, report(outPath))
			return nil
		},
	}
	c.Flags().StringVarP(&outPath, "output", "o", "hub.yaml", "settings file to write")
	c.Flags().BoolVar(&force, "force", false, "replace the file if it exists")
	return c
}

// runWizard asks the questions and returns the settings document and a function that renders the
// checklist for the file's name.
func runWizard(w *wizard) (settings.Document, func(string) string, error) {
	doc := settings.Document{Version: settings.Version}
	var secrets [][2]string // env var, what it holds
	var notes []string

	fmt.Fprintln(w.out, "DocTheRepo Hub setup. Press Enter to accept the [default].")
	fmt.Fprintln(w.out)
	labels := make([]string, len(targets))
	for i, t := range targets {
		labels[i] = t.label
	}
	tg := targets[w.choose("1. Where will the Hub run?", labels, 1)]
	url := strings.TrimRight(w.ask("2. The Hub's address, as people will open it", tg.defaultURL), "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return doc, nil, errors.New("the address must start with https:// (or http:// on your own computer)")
	}
	fmt.Fprintln(w.out)

	method := w.choose("3. How will people sign in?", []string{
		"Single sign-on with our company accounts (Google, Microsoft, Okta, Keycloak…)",
		"Email and password (you add people and send them a link)",
		"Both",
	}, map[bool]int{true: 2, false: 1}[tg.id == "laptop"])
	sso := method != 1
	password := method != 0
	doc.Auth = &settings.Auth{Password: &password}
	callback := url + "/api/v1/auth/callback"
	if sso {
		pl := make([]string, len(ssoPresets))
		for i, p := range ssoPresets {
			pl[i] = p.label
		}
		p := ssoPresets[w.choose("   Your identity provider:", pl, 1)]
		fmt.Fprintf(w.out, "   Create an app for the Hub in %s now:\n   Callback URL: %s\n", p.label, callback)
		for i, s := range p.steps {
			fmt.Fprintf(w.out, "     %d. %s\n", i+1, s)
		}
		fmt.Fprintln(w.out)
		issuer := p.issuer
		if issuer == "" {
			for {
				issuer = w.ask("   Issuer URL ("+p.issuerHint+")", "")
				if strings.HasPrefix(issuer, "https://") && !strings.Contains(issuer, "<") {
					break
				}
				fmt.Fprintln(w.out, "   It must be an https URL without <placeholders>.")
			}
		}
		clientID := w.ask("   Client ID", "")
		for clientID == "" {
			clientID = w.ask("   Client ID (required)", "")
		}
		domains := list(w.ask("   Allowed email domains, comma-separated (who may sign in)", ""))
		if len(domains) == 0 && p.id == "google" {
			notes = append(notes, "No allowed domains: any Google account could sign in. Add allowed_domains to auth.sso in the file.")
		}
		doc.Auth.SSO = &settings.SSO{Provider: p.id, Issuer: issuer, ClientID: clientID, ClientSecret: envRef("DTH_SECRET_OIDC_CLIENT_SECRET"), AllowedDomains: domains}
		secrets = append(secrets, [2]string{"DTH_SECRET_OIDC_CLIENT_SECRET", "the client secret of the " + p.label + " app"})
		fmt.Fprintln(w.out)
	}

	owners := list(w.ask("4. Owner email(s), comma-separated (full control of the Hub)", ""))
	for len(owners) == 0 {
		owners = list(w.ask("   At least one owner is needed", ""))
	}
	admins := list(w.ask("   Admins (optional: manage connectors, models and users)", ""))
	for _, e := range owners {
		doc.Users = append(doc.Users, settings.User{Email: e, Role: "owner"})
	}
	for _, e := range admins {
		doc.Users = append(doc.Users, settings.User{Email: e, Role: "admin"})
	}
	if password {
		notes = append(notes, "People without single sign-on need a password link: after the first sign-in, open Users & access and create one for them.")
	}
	fmt.Fprintln(w.out)

	ml := make([]string, len(modelChoices)+1)
	for i, m := range modelChoices {
		ml[i] = m.label
	}
	ml[len(modelChoices)] = "Later, in the Hub (Providers & routing)"
	mi := w.choose("5. Which model provider writes docs and answers questions?", ml, 1)
	if mi < len(modelChoices) {
		m := modelChoices[mi]
		prov := settings.Provider{Name: m.label, Kind: m.kind, Extra: map[string]string{}}
		if i := strings.Index(prov.Name, " ("); i > 0 {
			prov.Name = prov.Name[:i]
		}
		if m.keyEnv != "" {
			prov.APIKey = envRef(m.keyEnv)
			secrets = append(secrets, [2]string{m.keyEnv, "the " + prov.Name + " API key"})
		}
		for _, e := range m.extras {
			key, q, _ := strings.Cut(e, ":")
			v := w.ask("   "+q, "")
			if key == "base_url" {
				prov.BaseURL = v
			} else {
				prov.Extra[key] = v
			}
		}
		if len(prov.Extra) == 0 {
			prov.Extra = nil
		}
		model := m.model
		if model == "" {
			model = w.ask("   Model name for writing docs and answering", prov.Extra["deployment"])
		} else {
			model = w.ask("   Model for writing docs and answering", model)
		}
		doc.Providers = append(doc.Providers, prov)
		doc.Routes = map[string]settings.Route{}
		for _, f := range []string{"docgen", "qa", "decode", "suggest", "triage"} {
			md := model
			if f == "triage" && m.fast != "" {
				md = m.fast
			}
			doc.Routes[f] = settings.Route{Provider: prov.Name, Model: md}
		}
		fmt.Fprintln(w.out)
	}

	gi := w.choose("6. Where is your code?", []string{
		"GitHub: connect after deploying, with one click in the Hub (nothing needed now)",
		"GitHub, with a token now",
		"GitLab, with a token now",
		"Later",
	}, 1)
	switch gi {
	case 0:
		notes = append(notes, "After the first sign-in, open Connectors → Connect with GitHub; you need to be able to install apps in the GitHub organization.")
	case 1, 2:
		typ, envName, what := "github", "DTH_SECRET_GITHUB_TOKEN", "a GitHub token with Contents and Pull requests read/write"
		if gi == 2 {
			typ, envName, what = "gitlab", "DTH_SECRET_GITLAB_TOKEN", "a GitLab token with the api scope"
		}
		con := settings.Connector{Name: map[string]string{"github": "GitHub", "gitlab": "GitLab"}[typ], Type: typ, Credentials: envRef(envName)}
		if typ == "gitlab" {
			if base := w.ask("   GitLab URL", "https://gitlab.com"); base != "https://gitlab.com" {
				con.Config = map[string]string{"base_url": strings.TrimRight(base, "/") + "/api/v4/"}
			}
		}
		doc.Connectors = append(doc.Connectors, con)
		secrets = append(secrets, [2]string{envName, what})
		for _, r := range list(w.ask("   Repositories to document (owner/name, comma-separated)", "")) {
			doc.Repos = append(doc.Repos, settings.Repo{FullName: r, Connector: con.Name})
		}
	}

	report := func(file string) string {
		var b strings.Builder
		line := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
		line("Prerequisites (%s):", tg.label)
		for _, p := range tg.prereqs {
			line("  - %s", p)
		}
		if sso {
			line("  - The app in your identity provider, with the callback URL %s", callback)
		}
		if mi < len(modelChoices) && modelChoices[mi].keyEnv == "" {
			line("  - The Hub's cloud identity allowed to call %s.", modelChoices[mi].label)
		}
		line("")
		if len(secrets) > 0 {
			line("Secrets the file refers to (set them where the Hub runs; never commit them):")
			for _, s := range secrets {
				line("  %s  %s", s[0], s[1])
			}
			line("")
		}
		line("Deploy:")
		for _, l := range tg.deploy(file) {
			line("  %s", l)
		}
		line("")
		line("When the Hub starts it applies the file: sign-in, owners%s are in place before anyone signs in.",
			map[bool]string{true: ", models", false: ""}[mi < len(modelChoices)])
		for _, n := range notes {
			line("Note: %s", n)
		}
		line("More: docs/install.md, docs/users-and-sign-in.md, docs/settings-file.md")
		return b.String()
	}
	return doc, report, doc.Validate()
}
