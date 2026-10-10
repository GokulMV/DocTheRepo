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

// IgnoreMatcher reports the paths a .dthignore's own lines skip ("!" lines re-include). The built-in
// list is left out: it skips tests and CI files, which repository documents still read.
func IgnoreMatcher(content []byte) (func(string) bool, error) {
	ignore, keep := ParseIgnoreFile(content)
	ig, err := compileGlobs(ignore)
	if err != nil {
		return nil, err
	}
	kp, err := compileGlobs(keep)
	if err != nil {
		return nil, err
	}
	return func(p string) bool { return matchAny(ig, p) && !matchAny(kp, p) }, nil
}
