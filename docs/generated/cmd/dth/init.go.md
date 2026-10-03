<!-- dth:generated source="cmd/dth/init.go" — edit only inside dth:human blocks -->
# `cmd/dth/init.go`

<!-- dth:chunk 00a41dbecf5c4adc -->
## `target`

Represents a deployment target (laptop, Docker Compose, AWS, GCP, Kubernetes) with deployment instructions, prerequisites, and a URL template. The `deploy` function generates shell commands for that target given a settings filename.

<!-- dth:chunk d40863f892feec51 -->
## `ssoPreset`

Represents an OpenID Connect identity provider (Google, Microsoft, Okta, Keycloak, or generic) with setup instructions, issuer URL details, and required callback configuration steps.

<!-- dth:chunk 1f283bbb1ede3cf9 -->
## `modelChoice`

Represents a model provider choice (Anthropic, OpenAI, Azure, AWS Bedrock, Google Vertex, Ollama) with API key environment variable, model names for general and fast operations, and extra configuration parameters (like base URL or deployment name) that are asked interactively.

<!-- dth:chunk b7b6418071973d81 -->
## `wizard`

wizard asks questions on in and writes prompts to out.

<!-- dth:chunk 9b6a7e4ee1857e87 -->
## `wizard.ask`

Prompts the user for a string response, displaying the question with an optional default in brackets. Returns the user's input trimmed, or the default if the user pressed Enter without typing.

<!-- dth:chunk dc9e20f64b7556d3 -->
## `wizard.choose`

Prompts the user to choose from a numbered list of options, repeatedly asking until a valid selection (1 to length) is entered. Returns the 0-indexed choice. Prints a blank line after a valid selection.

<!-- dth:chunk bafdfd59fbb4c190 -->
## `wizard.yes`

Prompts the user for a yes/no answer, displaying the default as uppercase (Y/n or y/N). Returns true for 'y' prefix, false otherwise, or the default if the user pressed Enter.

<!-- dth:chunk 56fdbe46ef3a4a0e -->
## `list`

Splits a string by commas and spaces, returning a slice of non-empty trimmed items. Used to parse comma-separated lists like email addresses and repository names.

<!-- dth:chunk 40f906475467b8be -->
## `envRef`

Creates a settings.Secret that references an environment variable by name, with the format `${env:name}`. Used to defer secret values to runtime without embedding them in the settings file.

<!-- dth:chunk 21c95b72f3045785 -->
## `app.initCmd`

Creates the 'init' cobra command that guides users through Hub setup via a wizard, writing a settings file and deployment checklist. Accepts `-o/--output` (default hub.yaml) and `--force` to overwrite. Validates the output file doesn't exist unless `--force` is set, then marshals the settings and writes them with mode 0600.

<!-- dth:chunk ff1883b24d9c999b -->
## `runWizard`

Executes an interactive setup wizard that gathers deployment target, Hub URL, authentication method (SSO/password), owners, model provider, and Git connector details. Returns a settings.Document, a report-rendering function that formats prerequisites and deployment steps, and any validation error. Accumulates secrets and notes to include in the final checklist.

<!-- dth:chunk bc0a9f0390bb4a0b -->
## `__module__`

Global slices defining available deployment targets (laptop, Docker Compose, AWS, GCP, Kubernetes), identity provider presets (Google, Microsoft, Okta, Keycloak, generic OIDC), and model provider choices (Anthropic, OpenAI, Azure, Bedrock, Vertex, Ollama) with their configuration requirements and deployment instructions.
