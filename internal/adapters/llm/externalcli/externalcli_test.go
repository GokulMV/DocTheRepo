package externalcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var stubBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "stubengine")
	if err != nil {
		panic(err)
	}
	stubBin = filepath.Join(dir, "stubengine")
	out, err := exec.Command("go", "build", "-o", stubBin, "github.com/GokulMV/DocTheRepo/test/mocks/stubengine").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func engine(t *testing.T, mode string, extra map[string]string) *Engine {
	t.Helper()
	e := map[string]string{"command_template": stubBin + " {task_file} {result_file}", "work_dir": t.TempDir(), "env": "STUB_MODE=" + mode}
	for k, v := range extra {
		e[k] = v
	}
	eng, err := New(ports.ProviderConfig{Kind: "external_cli", Extra: e})
	require.NoError(t, err)
	return eng
}

func TestGenerate_Success(t *testing.T) {
	eng := engine(t, "ok", nil)
	assert.Equal(t, "external_cli", eng.Kind())
	assert.True(t, eng.ReportsUsage())
	res, err := eng.Generate(context.Background(), FixtureTask("job-1"))
	require.NoError(t, err)
	require.Len(t, res.Docs, 2)
	assert.Equal(t, "docs/generated/calc/calc.go.md", res.Docs[0].Path)
	require.NotNil(t, res.Usage)
	assert.Equal(t, "stub-1", res.Usage.Model)
	entries, _ := os.ReadDir(eng.baseDir)
	assert.Empty(t, entries, "task and result files are deleted after the job")
}

func TestGenerate_FailureModes(t *testing.T) {
	var se *ports.SchemaError
	_, err := engine(t, "badschema", nil).Generate(context.Background(), FixtureTask("j"))
	require.ErrorAs(t, err, &se)
	assert.Contains(t, se.Problems[0], "$.docs[0]")

	task := FixtureTask("j")
	task.RepairErrors = se.Problems
	_, err = engine(t, "badschema", nil).Generate(context.Background(), task)
	require.NoError(t, err, "the repair pass carries the schema errors back to the engine")

	_, err = engine(t, "noresult", nil).Generate(context.Background(), FixtureTask("j"))
	require.ErrorAs(t, err, &se)

	_, err = engine(t, "fail", nil).Generate(context.Background(), FixtureTask("j"))
	var pe *ports.PermanentError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, err.Error(), "model quota exceeded", "the engine's own error message is surfaced")

	_, err = engine(t, "sleep", map[string]string{"timeout": "300ms"}).Generate(context.Background(), FixtureTask("j"))
	_, isT := ports.AsTransient(err)
	assert.True(t, isT, "a timeout is transient: %v", err)

	_, err = engine(t, "ok", nil).Generate(context.Background(), FixtureTask("../escape"))
	require.ErrorAs(t, err, &pe)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	_, err = engine(t, "sleep", nil).Generate(ctx, FixtureTask("j"))
	assert.True(t, errors.Is(err, context.Canceled), "%v", err)
}

func TestNew_Validation(t *testing.T) {
	_, err := New(ports.ProviderConfig{Extra: map[string]string{"command_template": "agent run"}})
	assert.ErrorContains(t, err, "{task_file}")
	_, err = New(ports.ProviderConfig{Extra: map[string]string{"command_template": "a '{task_file} {result_file}"}})
	assert.ErrorContains(t, err, "unterminated")
	_, err = New(ports.ProviderConfig{Extra: map[string]string{"command_template": "a {task_file} {result_file}", "timeout": "soon"}})
	assert.ErrorContains(t, err, "timeout")
	e, err := New(ports.ProviderConfig{APIKey: "k", Extra: map[string]string{"command_template": "a {task_file} {result_file}", "reports_usage": "false"}})
	require.NoError(t, err)
	assert.False(t, e.ReportsUsage())
	assert.Contains(t, e.env, "DTH_ENGINE_API_KEY=k", "the key is passed via env, never argv")
}

func TestSplitArgs(t *testing.T) {
	cases := map[string][]string{
		`agent run --task {task_file}`:        {"agent", "run", "--task", "{task_file}"},
		`agent "with space" 'single $x' a\ b`: {"agent", "with space", "single $x", "a b"},
		`agent --msg="a \"q\"" ; rm -rf /`:    {"agent", `--msg=a "q"`, ";", "rm", "-rf", "/"},
		"  agent\t--x  ":                      {"agent", "--x"},
	}
	for in, want := range cases {
		got, err := SplitArgs(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := SplitArgs("   ")
	assert.Error(t, err)
	_, err = SplitArgs(`a \`)
	assert.Error(t, err)
}

func TestConformance(t *testing.T) {
	cfg := func(mode string) ports.ProviderConfig {
		return ports.ProviderConfig{Extra: map[string]string{"command_template": stubBin + " {task_file} {result_file}", "work_dir": t.TempDir(), "env": "STUB_MODE=" + mode}}
	}
	rep := Conformance(context.Background(), cfg("ok"))
	assert.True(t, rep.Passed, "%+v", rep.Checks)
	assert.False(t, rep.Unguarded)
	assert.Len(t, rep.Checks, 7)

	rep = Conformance(context.Background(), cfg("nousage"))
	assert.True(t, rep.Passed)
	assert.True(t, rep.Unguarded)

	rep = Conformance(context.Background(), cfg("outside"))
	assert.False(t, rep.Passed)
	rep = Conformance(context.Background(), cfg("badschema"))
	assert.False(t, rep.Passed)
	rep = Conformance(context.Background(), cfg("fail"))
	assert.False(t, rep.Passed)
	rep = Conformance(context.Background(), ports.ProviderConfig{})
	assert.False(t, rep.Passed)
}

func TestGenerate_ModelPlaceholder(t *testing.T) {
	out := filepath.Join(t.TempDir(), "model.txt")
	tmpl := `sh -c 'printf %s "$1" > "$2"; exec "$3" "$4" "$5"' sh {model} ` + out + " " + stubBin + " {task_file} {result_file}"
	eng := engine(t, "ok", map[string]string{"command_template": tmpl})
	task := FixtureTask("job-m")
	task.Model = "anthropic/claude-sonnet-5-5"
	_, err := eng.Generate(context.Background(), task)
	require.NoError(t, err)
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude-sonnet-5-5", string(got), "{model} is the route's model")
}
