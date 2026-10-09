// Package redact removes secrets and identifying names from text before it can
// leave the process (for example to a language model). It is deliberately
// conservative: when in doubt it removes.
package redact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type rule struct {
	class string
	re    *regexp.Regexp
	repl  string
}

var rules = []rule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(-----END [A-Z ]*PRIVATE KEY-----|$)`), "[REDACTED-PRIVATE-KEY]"},
	{"url-credentials", regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`), "${1}[REDACTED]@"},
	// Prefixed secrets use an explicit non-alphanumeric boundary instead of \b: underscore is a
	// word character, so \b let "x_<secret>" and "<secret>_" through (found by fuzzing).
	{"jwt", regexp.MustCompile(`(^|[^A-Za-z0-9])eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*`), "${1}[REDACTED-JWT]"},
	{"aws-key", regexp.MustCompile(`(^|[^A-Za-z0-9])(AKIA|ASIA|AGPA|AIDA|AROA)[0-9A-Z]{16}`), "${1}[REDACTED-KEY]"},
	{"github-token", regexp.MustCompile(`(^|[^A-Za-z0-9])((ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})`), "${1}[REDACTED-TOKEN]"},
	{"slack-token", regexp.MustCompile(`(^|[^A-Za-z0-9])xox[abprs]-[A-Za-z0-9-]{10,}`), "${1}[REDACTED-TOKEN]"},
	{"google-key", regexp.MustCompile(`(^|[^A-Za-z0-9])AIza[0-9A-Za-z_-]{30,}`), "${1}[REDACTED-KEY]"},
	{"api-key", regexp.MustCompile(`(^|[^A-Za-z0-9])(sk|pk|rk)-[A-Za-z0-9_-]{16,}`), "${1}[REDACTED-KEY]"},
	{"bearer", regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{12,}`), "${1} [REDACTED]"},
	{"assignment", regexp.MustCompile(`(?i)\b(pass(word|wd)?|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)s?\b(\s*[=:]\s*)("[^"]*"|'[^']*'|\S+)`), "${1}${3}[REDACTED]"},
	{"long-token", regexp.MustCompile(`\b[A-Za-z0-9+/_-]{40,}={0,2}\b`), "[REDACTED-TOKEN]"},
}

// Redactor scrubs text and pseudonymizes names. Counts are kept so the audit log can
// show how much was removed without ever storing the removed values.
type Redactor struct {
	Reveal bool // show real names instead of pseudonyms (secrets are still removed)
	names  map[string]string
	next   map[string]int
	Counts map[string]int
}

func New(reveal bool) *Redactor {
	return &Redactor{Reveal: reveal, names: map[string]string{}, next: map[string]int{}, Counts: map[string]int{}}
}

// Text removes secrets and control characters. It never returns the removed values.
func (r *Redactor) Text(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c == '\n' || c == '\t':
			b.WriteRune(c)
		case c < 0x20 || c == 0x7f:
			r.Counts["control"]++
		default:
			b.WriteRune(c)
		}
	}
	out := b.String()
	for _, ru := range rules {
		matches := ru.re.FindAllStringIndex(out, -1)
		if len(matches) > 0 {
			r.Counts[ru.class] += len(matches)
			out = ru.re.ReplaceAllString(out, ru.repl)
		}
	}
	return out
}

// Name returns a stable pseudonym for an identifying name (node, namespace, workload,
// pool, registry). The same name always maps to the same pseudonym within a Redactor.
func (r *Redactor) Name(kind, s string) string {
	if s == "" {
		return ""
	}
	if r.Reveal {
		return r.Text(s)
	}
	key := kind + "\x00" + s
	if p, ok := r.names[key]; ok {
		return p
	}
	r.next[kind]++
	p := fmt.Sprintf("%s-%d", kind, r.next[kind])
	r.names[key] = p
	r.Counts["name:"+kind]++
	return p
}

// Prime assigns pseudonyms in sorted order so numbering does not depend on iteration order.
func (r *Redactor) Prime(kind string, names []string) {
	sorted := append([]string{}, names...)
	sort.Strings(sorted)
	for _, n := range sorted {
		r.Name(kind, n)
	}
}

// Unmap replaces pseudonyms in model output with real names, for display to the user
// who owns the data. Longer pseudonyms are replaced first (node-10 before node-1).
func (r *Redactor) Unmap(s string) string {
	type pair struct{ p, real string }
	var pairs []pair
	for key, p := range r.names {
		pairs = append(pairs, pair{p, key[strings.IndexByte(key, 0)+1:]})
	}
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].p) > len(pairs[j].p) })
	for _, pr := range pairs {
		s = strings.ReplaceAll(s, pr.p, pr.real)
	}
	return s
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
}

// Mask replaces every known real name in free text with its pseudonym. Only whole
// tokens are replaced, and names shorter than 4 characters are skipped so ordinary
// words are not mangled; those short names are pseudonymized where they are structured data.
func (r *Redactor) Mask(s string) string {
	if r.Reveal || len(r.names) == 0 {
		return s
	}
	type pair struct{ real, p string }
	var pairs []pair
	for key, p := range r.names {
		real := key[strings.IndexByte(key, 0)+1:]
		if len(real) >= 4 {
			pairs = append(pairs, pair{real, p})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].real) > len(pairs[j].real) })
	for _, pr := range pairs {
		var b strings.Builder
		rest := s
		for {
			i := strings.Index(rest, pr.real)
			if i < 0 {
				b.WriteString(rest)
				break
			}
			end := i + len(pr.real)
			before := i == 0 || !isNameChar(rest[i-1])
			after := end == len(rest) || !isNameChar(rest[end])
			b.WriteString(rest[:i])
			if before && after {
				b.WriteString(pr.p)
				r.Counts["masked"]++
			} else {
				b.WriteString(pr.real)
			}
			rest = rest[end:]
		}
		s = b.String()
	}
	return s
}

// Scrub is Text followed by Mask: the single call used for everything that leaves the process.
func (r *Redactor) Scrub(s string) string { return r.Mask(r.Text(s)) }
