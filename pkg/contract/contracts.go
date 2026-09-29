package contract

// DocGenVersion is the current DocGen contract version. Evolution is additive-only: new fields are
// optional, unknown fields are ignored by both sides, and a breaking change bumps the major version with
// the previous major supported for at least one release (plan § 7.9).
const DocGenVersion = "2.0"

// ChunkToGenerate is one symbol the engine must document.
type ChunkToGenerate struct {
	ChunkID       string `json:"chunk_id"`
	Symbol        string `json:"symbol"`
	FilePath      string `json:"file_path"`
	ChangeType    string `json:"change_type"` // added | changed
	TargetDocPath string `json:"target_doc_path"`
}

// DocGenTask is the task file the Hub writes for an external engine.
type DocGenTask struct {
	ContractVersion  string            `json:"contract_version"`
	JobID            string            `json:"job_id"`
	Repo             string            `json:"repo"`
	CommitSHA        string            `json:"commit_sha"`
	DocsPath         string            `json:"docs_path"`
	ChunksToGenerate []ChunkToGenerate `json:"chunks_to_generate"`
	// Context is the scoped context (changed code, related signatures, team knowledge) as prompt text.
	Context string `json:"context"`
	// DiffSummaryPath points at a JSON file with the triage verdicts for the push.
	DiffSummaryPath string `json:"diff_summary_path,omitempty"`
	OutputPath      string `json:"output_path"`
	// RepairErrors is set on the single repair retry: the schema problems in the previous result.
	RepairErrors            []string `json:"repair_errors,omitempty"`
	MaxOutputTokensPerChunk int      `json:"max_output_tokens_per_chunk,omitempty"`
}

// GeneratedDoc is one section of generated documentation.
type GeneratedDoc struct {
	Path    string `json:"path"`
	ChunkID string `json:"chunk_id"`
	Symbol  string `json:"symbol"`
	Content string `json:"content"`
	// Summary optionally describes the whole source file (used for the doc file header and indexes).
	Summary string `json:"summary,omitempty"`
}

// ReportedUsage is what a conforming engine must report so the spend guard sees external spending.
type ReportedUsage struct {
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
}

// DocGenResult is the result file an engine writes before exiting 0.
type DocGenResult struct {
	ContractVersion string         `json:"contract_version"`
	Status          string         `json:"status"` // success | error
	Error           *string        `json:"error"`
	Docs            []GeneratedDoc `json:"docs"`
	Usage           *ReportedUsage `json:"usage,omitempty"`
	Extensions      map[string]any `json:"extensions,omitempty"`
}

// DocGenResultSchema validates result files (and direct-API doc generation output).
var DocGenResultSchema = Schema{
	"type":     "object",
	"required": []any{"contract_version", "status", "docs"},
	"properties": Schema{
		"contract_version": Schema{"type": "string", "pattern": `^2\.\d+$`},
		"status":           Schema{"type": "string", "enum": []any{"success", "error"}},
		"error":            Schema{"type": []any{"string", "null"}},
		"docs": Schema{"type": "array", "items": Schema{
			"type":     "object",
			"required": []any{"path", "chunk_id", "symbol", "content"},
			"properties": Schema{
				"path":     Schema{"type": "string", "minLength": 1},
				"chunk_id": Schema{"type": "string", "pattern": "^[0-9a-f]{16}$"},
				"symbol":   Schema{"type": "string", "minLength": 1},
				"content":  Schema{"type": "string", "minLength": 1},
				"summary":  Schema{"type": "string"},
			},
		}},
		"usage": Schema{"type": "object", "required": []any{"input_tokens", "output_tokens"}, "properties": Schema{
			"input_tokens":  Schema{"type": "integer", "minimum": 0},
			"output_tokens": Schema{"type": "integer", "minimum": 0},
			"provider":      Schema{"type": "string"},
			"model":         Schema{"type": "string"},
		}},
		"extensions": Schema{"type": "object"},
	},
}

// DocGenOutputSchema is what a direct-API model is asked to return (structured output): just the docs.
// Kept strict (additionalProperties:false) because providers with native structured output require it.
var DocGenOutputSchema = Schema{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"file_summary", "docs"},
	"properties": Schema{
		"file_summary": Schema{"type": "string"},
		"docs": Schema{"type": "array", "items": Schema{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"chunk_id", "symbol", "content"},
			"properties": Schema{
				"chunk_id": Schema{"type": "string"},
				"symbol":   Schema{"type": "string"},
				"content":  Schema{"type": "string"},
			},
		}},
	},
}

// DecodeResult is what an issue decode returns (plan § 7.9).
type DecodeResult struct {
	Summary       string `json:"summary"`
	ProbableCause string `json:"probable_cause"`
	Impact        string `json:"impact"`
	AffectedCode  []struct {
		ChunkID string `json:"chunk_id"`
		Reason  string `json:"reason"`
	} `json:"affected_code"`
	NextSteps         []string `json:"next_steps"`
	Confidence        string   `json:"confidence"`
	IsActionable      bool     `json:"is_actionable"`
	SuggestKnownIssue bool     `json:"suggest_known_issue"`
}

// DecodeSchema validates decode output.
var DecodeSchema = Schema{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"summary", "probable_cause", "impact", "affected_code", "next_steps", "confidence", "is_actionable", "suggest_known_issue"},
	"properties": Schema{
		"summary":        Schema{"type": "string", "minLength": 1},
		"probable_cause": Schema{"type": "string"},
		"impact":         Schema{"type": "string"},
		"affected_code": Schema{"type": "array", "items": Schema{
			"type": "object", "additionalProperties": false, "required": []any{"chunk_id", "reason"},
			"properties": Schema{"chunk_id": Schema{"type": "string"}, "reason": Schema{"type": "string"}},
		}},
		"next_steps":          Schema{"type": "array", "items": Schema{"type": "string"}},
		"confidence":          Schema{"type": "string", "enum": []any{"high", "medium", "low"}},
		"is_actionable":       Schema{"type": "boolean"},
		"suggest_known_issue": Schema{"type": "boolean"},
	},
}

// TriageVerdict is the LLM triage fallback's answer for a file with no grammar.
type TriageVerdict struct {
	Cosmetic  bool   `json:"cosmetic"`
	Confident bool   `json:"confident"`
	Reason    string `json:"reason"`
}

// TriageSchema validates LLM triage output.
var TriageSchema = Schema{
	"type": "object", "additionalProperties": false, "required": []any{"cosmetic", "confident", "reason"},
	"properties": Schema{
		"cosmetic": Schema{"type": "boolean"}, "confident": Schema{"type": "boolean"}, "reason": Schema{"type": "string"},
	},
}
