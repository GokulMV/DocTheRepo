package repodocs

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// decisionRE spots commits that record a decision: adopting, replacing or removing something.
var decisionRE = regexp.MustCompile(`(?i)\b(adr|decision|decide[ds]?|migrat(e|ed|ion)|replac(e|ed|es)|switch(ed)? (to|from)|adopt(ed)?|deprecat(e|ed|ion)|introduc(e|ed)|remov(e|ed) (the )?(support|dependency)|move[ds]? (to|from)|rewrit(e|ten))\b`)

// IsDecisionCommit reports a commit whose message records a decision.
func IsDecisionCommit(c ports.Commit) bool { return decisionRE.MatchString(c.Message) }

// IsADR reports architecture decision record files.
func IsADR(p string) bool {
	l := strings.ToLower(p)
	return strings.HasSuffix(l, ".md") && (strings.Contains(l, "/adr/") || strings.HasPrefix(l, "adr/") || strings.Contains(l, "/decisions/") || strings.Contains(l, "architecture-decision"))
}

func hasDecisions(f *Facts) bool {
	for _, c := range f.Commits {
		if IsDecisionCommit(c) {
			return true
		}
	}
	return hasPath(IsADR)(f)
}

// weekOf is the ISO year-week of the newest commit: Recent changes is rewritten at most once a week.
func weekOf(cs []ports.Commit) string {
	if len(cs) == 0 {
		return ""
	}
	y, w := cs[0].At.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// Section is one part of a document.
type Section struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	// Guide says what belongs in it (shown to the model).
	Guide string `json:"guide"`
	// Words is the length to aim for; replies over 1.6× fail the check. Tables and diagrams are not counted.
	Words    int  `json:"words"`
	Required bool `json:"required"`
	// Cite: claims about behaviour must cite path:line.
	Cite bool `json:"cite"`
}

// Spec is a document type.
type Spec struct {
	Type     string    `json:"type"`
	Title    string    `json:"title"`
	Group    string    `json:"group"` // Basics | Interfaces | Engineering | Operations | People
	Audience string    `json:"audience"`
	Purpose  string    `json:"purpose"`
	Sections []Section `json:"sections"`
	// PerModule: one document per module (module guides).
	PerModule bool `json:"per_module,omitempty"`
	// Needs lists the inputs the document is written from (see Inputs).
	Needs []string `json:"needs"`
	// Applies decides whether the repository has what this document describes.
	Applies func(f *Facts) bool `json:"-"`
	// Order sorts documents in the navigation.
	Order int `json:"order"`
}

// SpecVersion changes when the catalogue changes in a way that should rewrite documents.
// v2.3: module guides are given their tests' names and excerpts.
const SpecVersion = "v2.3"

func s(key, title string, words int, required, cite bool, guide string) Section {
	return Section{Key: key, Title: title, Words: words, Required: required, Cite: cite, Guide: guide}
}

func hasFact(kinds ...string) func(*Facts) bool {
	return func(f *Facts) bool {
		for _, x := range f.Facts {
			for _, k := range kinds {
				if x.Kind == k {
					return true
				}
			}
		}
		return false
	}
}

func hasPath(match func(string) bool) func(*Facts) bool {
	return func(f *Facts) bool {
		for _, p := range f.AllPaths {
			if match(p) {
				return true
			}
		}
		return false
	}
}

func always(*Facts) bool { return true }

// IsMigration reports database schema files (migrations, schema dumps, ORM models directories).
func IsMigration(p string) bool {
	l := strings.ToLower(p)
	return strings.Contains(l, "migration") && (strings.HasSuffix(l, ".sql") || strings.HasSuffix(l, ".py") || strings.HasSuffix(l, ".rb") || strings.HasSuffix(l, ".ts") || strings.HasSuffix(l, ".js") || strings.HasSuffix(l, ".go")) ||
		path.Base(l) == "schema.sql" || path.Base(l) == "schema.prisma" || strings.HasSuffix(l, ".prisma") || strings.HasPrefix(path.Base(l), "schema.rb")
}

// IsCI reports CI and deployment pipeline files.
func IsCI(p string) bool {
	l := strings.ToLower(p)
	b := path.Base(l)
	return strings.HasPrefix(l, ".github/workflows/") || b == ".gitlab-ci.yml" || b == "jenkinsfile" || strings.HasPrefix(l, ".circleci/") ||
		b == "azure-pipelines.yml" || b == "bitbucket-pipelines.yml" || b == "cloudbuild.yaml" || b == "buildspec.yml"
}

// IsDeploy reports infrastructure and deployment files.
func IsDeploy(p string) bool {
	l := strings.ToLower(p)
	b := path.Base(l)
	return b == "dockerfile" || strings.HasPrefix(b, "docker-compose") || strings.HasSuffix(l, ".tf") || strings.Contains(l, "/helm/") ||
		strings.HasPrefix(l, "helm/") || strings.Contains(l, "k8s/") || strings.Contains(l, "kubernetes/") || b == "chart.yaml" ||
		b == "fly.toml" || b == "app.yaml" || b == "serverless.yml" || b == "railway.json" || b == "procfile"
}

// Catalog is every document type, in navigation order. Overview, Architecture and the module guides
// are always written; the rest when the repository has what they describe.
var Catalog = []Spec{
	{
		Type: "overview", Title: "Overview", Group: "Basics", Order: 10, Applies: always,
		Audience: "everyone, including non-engineers", Purpose: "What this repository is, who it is for, and where to go next.",
		Needs: []string{"modules", "special", "facts_summary", "entry_points"},
		Sections: []Section{
			s("what", "What it does", 120, true, false, "What the software does and for whom, in plain words. No jargon without explaining it."),
			s("concepts", "Key concepts", 180, true, true, "The 4-8 ideas someone must know to understand the code (domain terms and main abstractions), one line each, with where they live."),
			s("map", "Module map", 0, true, false, "A table: Module | What it is for (one line). Every module, in a sensible reading order."),
			s("entry", "Entry points", 0, true, true, "A table: Entry point (binary, server, CLI command, worker, UI) | What starts it | Where ([path:line])."),
			s("run", "Running it", 150, false, true, "How to build, run and test it locally, from the README, Makefile, package scripts or compose files. Commands in code blocks."),
			s("next", "Where to go next", 80, true, false, "Which documents to read next for which goal (new engineer, product person, on-call)."),
		},
	},
	{
		Type: "architecture", Title: "Architecture", Group: "Basics", Order: 20, Applies: always,
		Audience: "engineers and technical leads; the style section also for non-engineers", Purpose: "How the whole application is built and how a request or job flows through it.",
		Needs: []string{"module_guides", "module_graph", "facts_summary", "special", "diagram"},
		Sections: []Section{
			s("style", "Architectural style", 250, true, true, "Name the style(s) the code follows (e.g. layered, hexagonal/ports and adapters, modular monolith, microservices, event-driven, MVC, CQRS). Say how you can tell from the code (with citations), the benefits it brings here, and one concrete example from this codebase. If it mixes styles, say where."),
			s("layers", "Layers and their responsibilities", 300, true, true, "Each layer or tier: what belongs there, what must not, which modules make it up, and an example."),
			s("components", "Components", 200, true, false, "Explain the diagram that is provided (do not redraw it): the main components and how they depend on each other."),
			s("flow", "End-to-end flow", 400, true, true, "The main paths through the application step by step (a request, a job, an event), naming the modules and functions involved, with a mermaid sequenceDiagram for the most important one."),
			s("data", "What is stored where", 200, true, true, "Each datastore, cache, queue or file store: what is kept there, who writes it, who reads it. A table if there are several."),
			s("cross", "Cross-cutting concerns", 200, false, true, "Authentication and authorization, errors, logging and metrics, configuration, concurrency: how each is handled across modules."),
			s("integrations", "External systems", 150, false, true, "Services, APIs and cloud resources it talks to, and why."),
			s("dos", "Do's and don'ts", 200, true, true, "Rules the code follows that a contributor must keep (as 'Do' and 'Don't' bullets), and any place the code breaks them."),
			s("tradeoffs", "Trade-offs and limits", 150, false, false, "What this design makes easy or hard, and known limits."),
		},
	},
	{
		Type: "module", Title: "Module guide", Group: "Modules", Order: 30, PerModule: true, Applies: always,
		Audience: "engineers working in this module", Purpose: "What this module is responsible for and how it works inside.",
		Needs: []string{"module", "module_graph"},
		Sections: []Section{
			s("purpose", "Purpose", 80, true, false, "Why this module exists, in two or three sentences."),
			s("responsibilities", "Responsibilities", 150, true, true, "What it is responsible for, as bullets, and what it deliberately does not do (and which module does)."),
			s("how", "How it works", 400, true, true, "The internal design: the main types and functions and how they work together, the order things happen in, and why. Use a mermaid diagram only if it helps."),
			s("interface", "Public interface", 0, true, true, "A table: Name | What it does | Notes. The functions, types, endpoints or commands other modules use."),
			s("data", "Data it owns", 150, false, true, "State, tables, caches or files this module owns, and their shape."),
			s("errors", "Errors and edge cases", 200, true, true, "What can go wrong, how it is reported or retried, and edge cases handled (or not)."),
			s("config", "Configuration", 0, false, true, "A table: Setting / env var | Effect | Default, for settings this module reads."),
			s("deps", "Depends on and used by", 0, true, false, "A table: Module | Direction (uses / used by) | For what."),
			s("files", "Files", 0, true, false, "A table: File | Role (one line). Every file in the module."),
			s("change", "Where to change things", 150, true, true, "5-8 bullets: 'To <common change>, edit `<function>` in [path:line]'. Every bullet cites where to start."),
			s("dos", "Do's and don'ts", 120, false, true, "Rules to keep when changing this module."),
			s("tests", "Tests", 100, false, true, "From the test names listed in the material: what the tests cover (name the main tests and cite each as [path:line]), how to run them (the package's test command), and the notable gaps: central code or error paths no listed test exercises."),
		},
	},
	{
		Type: "flows", Title: "Flows", Group: "Interfaces", Order: 40, Applies: hasFact("endpoint", "topic_sub", "topic_pub"),
		Audience: "engineers and on-call", Purpose: "The key paths through the system, step by step.",
		Needs: []string{"module_guides", "facts_detail", "central_bodies"},
		Sections: []Section{
			s("list", "Flows at a glance", 0, true, false, "A table: Flow | Trigger | Outcome, for the 3-8 most important flows."),
			s("details", "Each flow", 900, true, true, "For each flow a ### heading, then: trigger, numbered steps naming the function and [path:line], a mermaid sequenceDiagram, data touched, and what happens on failure."),
		},
	},
	{
		Type: "api", Title: "API reference", Group: "Interfaces", Order: 50, Applies: hasFact("endpoint"),
		Audience: "engineers and API consumers", Purpose: "Every endpoint the code exposes.",
		Needs: []string{"endpoints", "central_bodies"},
		Sections: []Section{
			s("overview", "Conventions", 150, true, true, "Base path, authentication, formats, pagination and error shape, as far as the code shows."),
			s("endpoints", "Endpoints", 0, true, true, "Group by resource with ### headings. For each endpoint: method and path, what it does, auth or role, request fields, response, errors, and the handler ([path:line]). Use tables for fields."),
		},
	},
	{
		Type: "events", Title: "Events and messages", Group: "Interfaces", Order: 60, Applies: hasFact("topic_pub", "topic_sub"),
		Audience: "engineers", Purpose: "Topics and queues: who publishes, who consumes, and what is in them.",
		Needs: []string{"topics", "central_bodies"},
		Sections: []Section{
			s("list", "Topics and queues", 0, true, true, "A table: Topic/queue | Producers | Consumers | Purpose."),
			s("payloads", "Payloads", 0, true, true, "For each topic a ### heading with the payload fields (table), keys and ordering, delivery guarantee, retries and dead-letter handling, as the code shows."),
		},
	},
	{
		Type: "data", Title: "Data model", Group: "Interfaces", Order: 70, Applies: func(f *Facts) bool { return hasFact("datastore")(f) || hasPath(IsMigration)(f) },
		Audience: "engineers", Purpose: "What is stored, its shape, and who owns it.",
		Needs: []string{"datastores", "migrations"},
		Sections: []Section{
			s("stores", "Stores", 0, true, true, "A table: Store | Technology | Owned by (module) | What is in it."),
			s("schema", "Tables and fields", 0, true, true, "For each table or collection a ### heading with columns (table), keys, indexes and relations."),
			s("diagram", "Relations", 0, false, false, "A mermaid erDiagram of the main tables."),
			s("history", "Schema history", 150, false, true, "How the schema evolved (migrations in order), and how to add a migration."),
		},
	},
	{
		Type: "config", Title: "Configuration", Group: "Engineering", Order: 80, Applies: hasFact("env_var"),
		Audience: "engineers and operators", Purpose: "Every setting and environment variable.",
		Needs: []string{"env", "special"},
		Sections: []Section{
			s("how", "How configuration works", 150, true, true, "Where settings come from (env, files, flags, defaults) and in which order."),
			s("vars", "Settings", 0, true, true, "A table: Name | Effect | Default | Secret? | Read in ([path:line]). Every environment variable."),
		},
	},
	{
		Type: "tests", Title: "Test architecture", Group: "Engineering", Order: 90, Applies: hasPath(IsTest),
		Audience: "engineers", Purpose: "How the code is tested and how to run the tests.",
		Needs: []string{"tests", "special"},
		Sections: []Section{
			s("layers", "Test layers", 200, true, true, "Unit, integration, end-to-end and others: frameworks, where each lives, what each covers."),
			s("run", "Running tests", 120, true, true, "Commands to run all tests, one package, and the slow suites; prerequisites (Docker, databases)."),
			s("fixtures", "Fixtures and fakes", 150, false, true, "Test helpers, fakes, fixtures and test data, and how to add one."),
			s("coverage", "What is covered", 0, true, false, "A table: Module | Tests | Notes; and the gaps (modules with no tests)."),
		},
	},
	{
		Type: "build", Title: "Build, CI and deploy", Group: "Engineering", Order: 100, Applies: func(f *Facts) bool { return hasPath(IsCI)(f) || hasPath(IsDeploy)(f) },
		Audience: "engineers and operators", Purpose: "How the code is built, checked and released.",
		Needs: []string{"ci", "special"},
		Sections: []Section{
			s("build", "Building", 150, true, true, "How to build artifacts (binaries, images, bundles)."),
			s("ci", "Pipelines", 0, true, true, "A table: Pipeline/job | Trigger | What it does. Then notes on required checks."),
			s("deploy", "Deploying", 200, false, true, "Environments, how a release reaches them, and infrastructure as code."),
		},
	},
	{
		Type: "errors", Title: "Error catalogue", Group: "Engineering", Order: 110, Applies: always,
		Audience: "engineers and on-call", Purpose: "The errors the code raises, what they mean, and how to fix them.",
		Needs: []string{"error_symbols", "module_guides"},
		Sections: []Section{
			s("model", "How errors work", 150, true, true, "The error types, codes and wrapping conventions, and how errors reach users or logs."),
			s("catalogue", "Errors", 0, true, true, "A table: Error / code | Raised in ([path:line]) | Meaning | What to do."),
		},
	},
	{
		Type: "getting-started", Title: "Getting started", Group: "Operations", Order: 120, Applies: always,
		Audience: "new engineers", Purpose: "From a fresh checkout to a running app.",
		Needs: []string{"special", "env"},
		Sections: []Section{
			s("prereq", "Prerequisites", 100, true, false, "Tools and versions needed."),
			s("steps", "Steps", 250, true, true, "Numbered steps with commands, from clone to a running app."),
			s("verify", "Check it works", 80, false, false, "How to tell it is running."),
			s("trouble", "Common problems", 120, false, false, "Likely problems and fixes."),
		},
	},
	{
		Type: "operations", Title: "Runbooks and observability", Group: "Operations", Order: 130, Applies: func(f *Facts) bool { return hasPath(IsDeploy)(f) || hasFact("endpoint", "topic_sub")(f) },
		Audience: "on-call engineers", Purpose: "How to watch it and what to do when it breaks.",
		Needs: []string{"module_guides", "special", "error_symbols"},
		Sections: []Section{
			s("signals", "Logs, metrics and health", 200, true, true, "What it logs, metrics it exposes, health checks."),
			s("incidents", "Common incidents", 300, true, true, "For each likely failure a ### heading: symptoms, cause, steps to fix."),
			s("rollback", "Rollback and recovery", 120, false, false, "How to roll back a release and recover data."),
		},
	},
	{
		Type: "security", Title: "Security and access", Group: "Operations", Order: 140, Applies: always,
		Audience: "engineers and security reviewers", Purpose: "How access is controlled and data protected.",
		Needs: []string{"module_guides", "env", "endpoints"},
		Sections: []Section{
			s("authn", "Authentication", 150, true, true, "How users and services prove who they are."),
			s("authz", "Authorization", 150, true, true, "Roles and permission checks, and where they are enforced."),
			s("data", "Data protection", 150, false, true, "Secrets handling, encryption, personal data."),
			s("surface", "Attack surface", 0, false, true, "A table: Entry point | Exposure | Protection."),
		},
	},
	{
		Type: "integrations", Title: "Integrations", Group: "Operations", Order: 150, Applies: hasFact("dependency", "cloud_resource", "service", "datastore"),
		Audience: "engineers and on-call", Purpose: "External systems and what breaks if each is down.",
		Needs: []string{"deps", "module_guides"},
		Sections: []Section{
			s("list", "External systems", 0, true, true, "A table: System | Used for | Used in (module) | If it is down."),
			s("libs", "Key libraries", 0, false, false, "A table: Library | Why it is used. Only the libraries that shape the design."),
		},
	},
	{
		Type: "glossary", Title: "Glossary", Group: "People", Order: 160, Applies: always,
		Audience: "everyone, including non-engineers", Purpose: "The words this codebase and its team use.",
		Needs: []string{"module_guides"},
		Sections: []Section{
			s("terms", "Terms", 0, true, false, "A table: Term | Meaning in plain words | Where it appears. 10-40 domain and technical terms."),
		},
	},
	{
		Type: "ownership", Title: "Ownership and contributing", Group: "People", Order: 170, Applies: always,
		Audience: "everyone", Purpose: "Who owns what and how to contribute.",
		Needs: []string{"owners", "special", "modules"},
		Sections: []Section{
			s("owners", "Owners", 0, true, false, "A table: Area | Owner, from CODEOWNERS and the graph; say when unknown."),
			s("conventions", "Conventions", 200, true, true, "Code style, naming, branching and review conventions the repository follows."),
			s("feature", "Adding a feature", 200, true, true, "The usual steps and files to touch to add a typical feature."),
		},
	},
}

// History documents, written from commits (and decision record files).
var historySpecs = []Spec{
	{
		Type: "changes", Title: "Recent changes", Group: "People", Order: 180, Applies: func(f *Facts) bool { return len(f.Commits) > 0 },
		Audience: "everyone, including non-engineers", Purpose: "What changed recently, in plain words.",
		Needs: []string{"commits", "modules"},
		Sections: []Section{
			s("summary", "This period in short", 150, true, false, "What changed and why it matters, for someone who was away. No commit hashes."),
			s("areas", "Changes by area", 0, true, false, "A table: Area (module) | What changed | When. Group related commits."),
			s("notable", "Worth knowing", 150, false, false, "Breaking changes, new features, removals and risky changes, as bullets."),
		},
	},
	{
		Type: "decisions", Title: "Decision records", Group: "People", Order: 190, Applies: hasDecisions,
		Audience: "engineers and technical leads", Purpose: "The technical decisions visible in the history, with their context and consequences. Inferred from commits unless a decision record file exists.",
		Needs: []string{"decisions", "modules"},
		Sections: []Section{
			s("list", "Decisions", 0, true, false, "A table: Decision | When | Source (commit short hash or record file) | Inferred? (yes when only a commit message supports it)."),
			s("records", "Records", 700, true, false, "For each decision a ### heading, then Context, Decision, Consequences. Say \"(inferred from the commit message)\" when it is not written down anywhere."),
		},
	},
}

func init() { Catalog = append(Catalog, historySpecs...) }

// SpecByType finds a document type.
func SpecByType(t string) (Spec, bool) {
	for _, s := range Catalog {
		if s.Type == t {
			return s, true
		}
	}
	return Spec{}, false
}
