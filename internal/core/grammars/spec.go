// Package grammars maps file extensions to Tree-sitter grammars and the per-language node-type rules the
// chunker, triage, and palace extractors need. Five grammars are compiled in; more are loaded at startup
// from a directory of shared libraries, each with a JSON spec (plan § 8.1, § 8.5).
package grammars

// Spec describes how to read one language's syntax tree. Every list holds Tree-sitter node kinds.
type Spec struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
	// Symbol overrides the exported C symbol for runtime-loaded grammars (default tree_sitter_<name>).
	Symbol string `json:"symbol,omitempty"`

	// Definitions maps node kinds that become chunks to a definition kind
	// (function, method, class, type, interface, enum, trait, impl, module, record).
	Definitions map[string]string `json:"definitions"`
	// Wrappers are nodes that wrap a definition and should be included in its chunk (decorators, export).
	// The value is the field name holding the wrapped definition.
	Wrappers map[string]string `json:"wrappers,omitempty"`
	// FunctionDeclarators are declarator nodes whose value (field "value") is a function expression,
	// e.g. TS `const f = () => {}`; the value kinds that qualify are listed in FunctionValues.
	FunctionDeclarators []string `json:"function_declarators,omitempty"`
	FunctionValues      []string `json:"function_values,omitempty"`
	// NameFields are tried in order to find a definition's name (default: ["name"]).
	NameFields []string `json:"name_fields,omitempty"`
	// BodyFields hold a definition's body; the signature is the text before the body.
	BodyFields []string `json:"body_fields,omitempty"`
	// Scopes are definition kinds whose nested definitions get a qualified name (Class.method).
	Scopes []string `json:"scopes,omitempty"`

	Comments []string `json:"comments"`
	Strings  []string `json:"strings"`
	// SignificantStringParents are ancestors that make a string literal structurally meaningful
	// (route annotations, decorators, import paths) instead of cosmetic.
	SignificantStringParents []string `json:"significant_string_parents,omitempty"`

	// Calls maps call-expression node kinds to the field holding the callee.
	Calls map[string]string `json:"calls,omitempty"`
	// CallArgsField is the field holding call arguments (default "arguments").
	CallArgsField string `json:"call_args_field,omitempty"`
	// Imports are import/include statement kinds.
	Imports []string `json:"imports,omitempty"`
	// Annotations are decorator/annotation node kinds attached to definitions (routes, listeners).
	Annotations []string `json:"annotations,omitempty"`

	// EnvCallees are callee texts whose first string argument names an environment variable.
	EnvCallees []string `json:"env_callees,omitempty"`
	// EnvObjects are object expressions whose member/subscript names an environment variable
	// (process.env.X, os.environ["X"]).
	EnvObjects []string `json:"env_objects,omitempty"`
}

func set(kinds ...string) []string { return kinds }

// builtinSpecs are the node-type maps for the compiled-in grammars, derived from each grammar's
// node-types (see grammars_test.go, which asserts them against real parses).
var builtinSpecs = map[string]Spec{
	"go": {
		Name: "go", Extensions: set(".go"),
		Definitions: map[string]string{
			"function_declaration": "function", "method_declaration": "method", "type_spec": "type",
		},
		BodyFields:               set("body"),
		Comments:                 set("comment"),
		Strings:                  set("interpreted_string_literal", "raw_string_literal"),
		SignificantStringParents: set("import_spec"),
		Calls:                    map[string]string{"call_expression": "function"},
		Imports:                  set("import_declaration"),
		EnvCallees:               set("os.Getenv", "os.LookupEnv", "syscall.Getenv"),
	},
	"java": {
		Name: "java", Extensions: set(".java"),
		Definitions: map[string]string{
			"class_declaration": "class", "interface_declaration": "interface", "enum_declaration": "enum",
			"record_declaration": "record", "annotation_type_declaration": "interface",
			"method_declaration": "method", "constructor_declaration": "method",
		},
		BodyFields:               set("body"),
		Scopes:                   set("class_declaration", "interface_declaration", "enum_declaration", "record_declaration"),
		Comments:                 set("line_comment", "block_comment"),
		Strings:                  set("string_literal"),
		SignificantStringParents: set("annotation", "marker_annotation", "import_declaration"),
		Calls:                    map[string]string{"method_invocation": "name", "object_creation_expression": "type"},
		Imports:                  set("import_declaration"),
		Annotations:              set("annotation", "marker_annotation"),
		EnvCallees:               set("System.getenv"),
	},
	"python": {
		Name: "python", Extensions: set(".py", ".pyi"),
		Definitions:              map[string]string{"function_definition": "function", "class_definition": "class"},
		Wrappers:                 map[string]string{"decorated_definition": "definition"},
		BodyFields:               set("body"),
		Scopes:                   set("class_definition"),
		Comments:                 set("comment"),
		Strings:                  set("string", "concatenated_string"),
		SignificantStringParents: set("decorator", "import_statement", "import_from_statement"),
		Calls:                    map[string]string{"call": "function"},
		Imports:                  set("import_statement", "import_from_statement"),
		Annotations:              set("decorator"),
		EnvCallees:               set("os.getenv", "os.environ.get", "environ.get", "getenv"),
		EnvObjects:               set("os.environ", "environ"),
	},
	"typescript": {
		Name: "typescript", Extensions: set(".ts", ".mts", ".cts"),
		Definitions: map[string]string{
			"function_declaration": "function", "generator_function_declaration": "function",
			"class_declaration": "class", "abstract_class_declaration": "class",
			"method_definition": "method", "interface_declaration": "interface",
			"type_alias_declaration": "type", "enum_declaration": "enum",
		},
		Wrappers:                 map[string]string{"export_statement": "declaration"},
		FunctionDeclarators:      set("variable_declarator"),
		FunctionValues:           set("arrow_function", "function_expression"),
		BodyFields:               set("body"),
		Scopes:                   set("class_declaration", "abstract_class_declaration"),
		Comments:                 set("comment"),
		Strings:                  set("string", "template_string"),
		SignificantStringParents: set("decorator", "import_statement"),
		Calls:                    map[string]string{"call_expression": "function", "new_expression": "constructor"},
		Imports:                  set("import_statement"),
		Annotations:              set("decorator"),
		EnvObjects:               set("process.env", "import.meta.env"),
	},
	"tsx": {}, // filled from typescript in init
	"javascript": {
		Name: "javascript", Extensions: set(".js", ".mjs", ".cjs", ".jsx"),
		Definitions: map[string]string{
			"function_declaration": "function", "generator_function_declaration": "function",
			"class_declaration": "class", "method_definition": "method",
		},
		Wrappers:                 map[string]string{"export_statement": "declaration"},
		FunctionDeclarators:      set("variable_declarator"),
		FunctionValues:           set("arrow_function", "function_expression"),
		BodyFields:               set("body"),
		Scopes:                   set("class_declaration"),
		Comments:                 set("comment"),
		Strings:                  set("string", "template_string"),
		SignificantStringParents: set("decorator", "import_statement"),
		Calls:                    map[string]string{"call_expression": "function", "new_expression": "constructor"},
		Imports:                  set("import_statement"),
		Annotations:              set("decorator"),
		EnvObjects:               set("process.env", "import.meta.env"),
	},
	"rust": {
		Name: "rust", Extensions: set(".rs"),
		Definitions: map[string]string{
			"function_item": "function", "function_signature_item": "function", "struct_item": "type",
			"enum_item": "enum", "trait_item": "trait", "impl_item": "impl", "mod_item": "module",
			"type_item": "type", "union_item": "type",
		},
		NameFields:               set("name", "type"),
		BodyFields:               set("body"),
		Scopes:                   set("impl_item", "trait_item", "mod_item"),
		Comments:                 set("line_comment", "block_comment"),
		Strings:                  set("string_literal", "raw_string_literal"),
		SignificantStringParents: set("attribute_item", "use_declaration"),
		Calls:                    map[string]string{"call_expression": "function", "macro_invocation": "macro"},
		Imports:                  set("use_declaration"),
		Annotations:              set("attribute_item"),
		EnvCallees:               set("std::env::var", "env::var", "std::env::var_os", "env::var_os", "env!", "option_env!"),
	},
}

func init() {
	tsx := builtinSpecs["typescript"]
	tsx.Name = "tsx"
	tsx.Extensions = set(".tsx")
	builtinSpecs["tsx"] = tsx
}
