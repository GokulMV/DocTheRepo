package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// renderStream prints an Ask SSE stream: text as it arrives, then the numbered sources. The final
// answer (with fabricated citations removed) replaces the streamed text only when they differ.
func renderStream(r io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var event, streamed strings.Builder
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		payload := strings.Join(data, "\n")
		data = nil
		switch event.String() {
		case "delta":
			var d struct{ Text string }
			if json.Unmarshal([]byte(payload), &d) == nil {
				streamed.WriteString(d.Text)
				fmt.Fprint(out, d.Text)
			}
		case "done":
			var d struct {
				Answer    string
				Citations []struct {
					N                       int
					Type, Title, Repo, Path string
					URL                     string
				}
				Cached bool
			}
			if err := json.Unmarshal([]byte(payload), &d); err != nil {
				return err
			}
			if strings.TrimSpace(d.Answer) != strings.TrimSpace(streamed.String()) {
				fmt.Fprintf(out, "\n\n%s", d.Answer)
			}
			fmt.Fprintln(out)
			if len(d.Citations) > 0 {
				fmt.Fprintln(out, "\nSources:")
				for _, c := range d.Citations {
					loc := c.Path
					if c.Repo != "" {
						loc = c.Repo + "/" + c.Path
					}
					if c.URL != "" {
						loc = c.URL
					}
					fmt.Fprintf(out, "  [%d] %s (%s)\n", c.N, loc, c.Type)
				}
			}
			if d.Cached {
				fmt.Fprintln(out, "(cached answer)")
			}
		case "error":
			var e struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			_ = json.Unmarshal([]byte(payload), &e)
			return fmt.Errorf("%s: %s", e.Error.Code, e.Error.Message)
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
			event.Reset()
		case strings.HasPrefix(line, "event:"):
			event.Reset()
			event.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "event:")))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}
