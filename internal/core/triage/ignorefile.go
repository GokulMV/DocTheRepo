package triage

import "strings"

// IgnoreFile is the repository file listing paths the Hub skips: not documented, not indexed. It uses
// .gitignore syntax.
const IgnoreFile = ".dthignore"

// ParseIgnoreFile turns .gitignore-style lines into Ignore and Keep globs:
//
//	# comment            blank lines and comments are skipped
//	secrets.go           a name without "/" matches at any depth (a file, or a directory and all in it)
//	/scripts             a leading "/" anchors to the repository root
//	generated/           a trailing "/" matches only a directory, and everything in it
//	internal/legacy/**   a "/" inside anchors to the root; *, ? and ** work as in .gitignore
//	!keep.go             re-includes a path an earlier line (or the built-in list) skipped
func ParseIgnoreFile(content []byte) (ignore, keep []string) {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(line, "!")
		line = strings.TrimPrefix(line, `\`) // "\#name" and "\!name" are literal
		dirOnly := strings.HasSuffix(line, "/")
		rooted := strings.HasPrefix(line, "/")
		line = strings.Trim(line, "/")
		if line == "" {
			continue
		}
		anchored := rooted || strings.HasPrefix(line, "**/") || strings.Contains(line, "/")
		base := line
		if !anchored {
			base = "**/" + line
		}
		globs := []string{base + "/**"}
		if !dirOnly {
			globs = append(globs, base)
		}
		if neg {
			keep = append(keep, globs...)
		} else {
			ignore = append(ignore, globs...)
		}
	}
	return ignore, keep
}
