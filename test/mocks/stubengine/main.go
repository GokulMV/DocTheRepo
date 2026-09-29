// Command stubengine is a DocGen-contract engine for tests and load runs. It reads the task file and writes
// a result file without calling any model. STUB_MODE selects behaviour:
//
//	ok (default) | nousage | badschema | noresult | fail | outside | sleep
//
// STUB_LATENCY (a Go duration) delays every run, for burst-drain performance tests.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: stubengine <task_file> <result_file>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read task:", err)
		os.Exit(2)
	}
	var task contract.DocGenTask
	if err := json.Unmarshal(raw, &task); err != nil {
		fmt.Fprintln(os.Stderr, "parse task:", err)
		os.Exit(2)
	}
	if !strings.HasPrefix(task.ContractVersion, "2.") {
		fmt.Fprintln(os.Stderr, "unsupported contract version", task.ContractVersion)
		os.Exit(3)
	}
	if d, err := time.ParseDuration(os.Getenv("STUB_LATENCY")); err == nil {
		time.Sleep(d)
	}
	mode := os.Getenv("STUB_MODE")
	if mode == "badschema" && len(task.RepairErrors) > 0 {
		mode = "ok" // a repair pass that shows the errors fixes the output, as real engines usually do
	}
	switch mode {
	case "sleep":
		time.Sleep(time.Hour)
	case "fail":
		msg := "model quota exceeded"
		write(os.Args[2], contract.DocGenResult{ContractVersion: task.ContractVersion, Status: "error", Error: &msg, Docs: []contract.GeneratedDoc{}})
		os.Exit(1)
	case "noresult":
		return
	case "badschema":
		_ = os.WriteFile(os.Args[2], []byte(`{"contract_version":"2.0","status":"success","docs":[{"path":"x"}]}`), 0o600)
		return
	}
	res := contract.DocGenResult{ContractVersion: task.ContractVersion, Status: "success"}
	for _, c := range task.ChunksToGenerate {
		p := c.TargetDocPath
		if mode == "outside" {
			p = "README.md"
		}
		res.Docs = append(res.Docs, contract.GeneratedDoc{Path: p, ChunkID: c.ChunkID, Symbol: c.Symbol,
			Content: fmt.Sprintf("`%s` in `%s` (%s).", c.Symbol, c.FilePath, c.ChangeType), Summary: "Stub summary."})
	}
	if mode != "nousage" {
		res.Usage = &contract.ReportedUsage{InputTokens: int64(len(task.Context) / 4), OutputTokens: int64(20 * len(res.Docs)), Provider: "stub", Model: "stub-1"}
	}
	write(os.Args[2], res)
}

func write(path string, res contract.DocGenResult) {
	b, _ := json.Marshal(res)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write result:", err)
		os.Exit(2)
	}
}
