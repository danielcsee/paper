package pathmatch

import (
	"regexp"
	"strings"
)

// Match implements the path-glob subset used by adapters. ** crosses directory
// boundaries; * and ? do not. Paths and patterns always use slash separators.
func Match(pattern, name string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i += 2
				if i < len(pattern) && pattern[i] == '/' {
					b.WriteString("(?:.*/)?")
					i++
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
				i++
			}
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(pattern[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String()).MatchString(name)
}

func Included(name string, includes, excludes []string) bool {
	included := len(includes) == 0
	for _, pattern := range includes {
		if Match(pattern, name) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range excludes {
		if Match(pattern, name) {
			return false
		}
	}
	return true
}
