// Package loglines turns one application log message (CloudWatch Logs, GCP Cloud Logging) into signal
// fields: structured JSON logs are read field by field; plain text is scanned for a level word, an
// exception type, and a stack trace (Python, Java/Kotlin, Node.js, Go).
package loglines

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Parsed is what a log line says.
type Parsed struct {
	Level         string // raw level word ("ERROR", "error", "FATAL"); map with signals.MapSeverity
	Message       string
	ExceptionType string
	Stack         []ports.StackFrame
	Fields        map[string]string // top-level scalar fields of a JSON log line
}

var (
	levelRE      = regexp.MustCompile(`\b(FATAL|PANIC|CRITICAL|CRIT|SEVERE|ERROR|ERR|WARNING|WARN|NOTICE|INFO|DEBUG|TRACE)\b`)
	pyFrameRE    = regexp.MustCompile(`File "([^"]+)", line (\d+), in (\S+)`)
	pyExcRE      = regexp.MustCompile(`(?m)^([A-Za-z_][\w.]*(?:Error|Exception|Exit|Interrupt|Warning|Fault)):?\s?(.*)$`)
	javaExcRE    = regexp.MustCompile(`(?m)^(?:Exception in thread "[^"]*" )?(?:Caused by: )?([a-zA-Z_$][\w$]*(?:\.[a-zA-Z_$][\w$]*)+(?:Exception|Error|Throwable))(?::\s*(.*))?$`)
	javaFrameRE  = regexp.MustCompile(`(?m)^\s+at ([\w$.<>/]+)\.([\w$<>-]+)\(([^:)]*)(?::(\d+))?\)`)
	nodeFrameRE  = regexp.MustCompile(`(?m)^\s+at (?:(\S+) \()?(/?[^\s():]+):(\d+):\d+\)?\s*$`)
	nodeExcRE    = regexp.MustCompile(`(?m)^(\w*Error): (.*)$`)
	goPanicRE    = regexp.MustCompile(`(?m)^panic: (.*)$`)
	goFuncRE     = regexp.MustCompile(`(?m)^([\w./*()\-]+)\(.*\)\n\t(\S+\.go):(\d+)`)
	jsonMsgKeys  = []string{"message", "msg", "error.message", "err", "error", "textPayload"}
	jsonLvlKeys  = []string{"level", "severity", "lvl", "log.level", "levelname"}
	jsonExcKeys  = []string{"exception.type", "error.type", "error.kind", "exc_type", "exception_class", "error_class"}
	jsonStackKey = []string{"stack", "stack_trace", "stacktrace", "exception.stacktrace", "error.stack", "exc_info", "traceback"}
)

// Parse reads one log message.
func Parse(message string) Parsed {
	msg := strings.TrimSpace(message)
	if strings.HasPrefix(msg, "{") {
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(msg))
		dec.UseNumber()
		if dec.Decode(&obj) == nil {
			return parseJSON(obj)
		}
	}
	return parseText(msg)
}

func parseJSON(obj map[string]any) Parsed {
	p := Parsed{Fields: map[string]string{}}
	for k, v := range obj {
		if s := scalar(v); s != "" {
			p.Fields[k] = s
		}
	}
	get := func(keys []string) string {
		for _, k := range keys {
			if s := scalar(dig(obj, k)); s != "" {
				return s
			}
		}
		return ""
	}
	p.Level = get(jsonLvlKeys)
	p.Message = get(jsonMsgKeys)
	p.ExceptionType = get(jsonExcKeys)
	if st := get(jsonStackKey); st != "" {
		t := parseText(st)
		p.Stack = t.Stack
		if p.ExceptionType == "" {
			p.ExceptionType = t.ExceptionType
		}
	}
	return p
}

func parseText(msg string) Parsed {
	p := Parsed{Message: msg}
	if m := levelRE.FindStringSubmatch(msg); m != nil {
		p.Level = m[1]
	}
	switch {
	case strings.Contains(msg, "Traceback (most recent call last)"):
		// Python lists the outermost frame first; the top of the stack is the last frame.
		frames := pyFrameRE.FindAllStringSubmatch(msg, -1)
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			p.Stack = append(p.Stack, ports.StackFrame{Module: f[1], File: f[1], Line: atoi(f[2]), Function: f[3]})
		}
		tail := msg[strings.LastIndex(msg, "Traceback"):]
		if ms := pyExcRE.FindAllStringSubmatch(tail, -1); len(ms) > 0 {
			last := ms[len(ms)-1]
			p.ExceptionType, p.Message = last[1], strings.TrimSpace(last[2])
		}
	case javaFrameRE.MatchString(msg):
		for _, f := range javaFrameRE.FindAllStringSubmatch(msg, -1) {
			p.Stack = append(p.Stack, ports.StackFrame{Module: f[1], Function: f[2], File: f[3], Line: atoi(f[4])})
		}
		if m := javaExcRE.FindStringSubmatch(msg); m != nil {
			p.ExceptionType = m[1]
			if m[2] != "" {
				p.Message = strings.TrimSpace(m[2])
			}
		}
	case nodeFrameRE.MatchString(msg):
		for _, f := range nodeFrameRE.FindAllStringSubmatch(msg, -1) {
			p.Stack = append(p.Stack, ports.StackFrame{Function: f[1], Module: f[2], File: f[2], Line: atoi(f[3])})
		}
		if m := nodeExcRE.FindStringSubmatch(msg); m != nil {
			p.ExceptionType, p.Message = m[1], strings.TrimSpace(m[2])
		}
	case goPanicRE.MatchString(msg):
		p.ExceptionType = "panic"
		p.Message = goPanicRE.FindStringSubmatch(msg)[1]
		for _, f := range goFuncRE.FindAllStringSubmatch(msg, -1) {
			fn := f[1]
			mod, name := fn, fn
			if i := strings.LastIndex(fn, "."); i > 0 {
				mod, name = fn[:i], fn[i+1:]
			}
			p.Stack = append(p.Stack, ports.StackFrame{Module: mod, Function: name, File: f[2], Line: atoi(f[3])})
		}
		if p.Level == "" {
			p.Level = "PANIC"
		}
	}
	return p
}

// dig reads "a.b" either as a nested path or as a literal dotted key.
func dig(obj map[string]any, key string) any {
	if v, ok := obj[key]; ok {
		return v
	}
	var cur any = obj
	for _, seg := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[seg]
	}
	return cur
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// FirstLine is a message's first line (titles).
func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
