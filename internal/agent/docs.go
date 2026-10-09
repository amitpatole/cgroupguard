package agent

import (
	"sort"
	"strings"
	"unicode"

	"github.com/amitpatole/cgroupguard/docs"
)

type DocSection struct {
	File string
	Text string
}

// LoadDocs splits the embedded notes into paragraphs and table rows.
func LoadDocs() []DocSection {
	var out []DocSection
	entries, _ := docs.FS.ReadDir(".")
	for _, e := range entries {
		data, err := docs.FS.ReadFile(e.Name())
		if err != nil {
			continue
		}
		for _, part := range strings.Split(string(data), "\n") {
			if t := strings.TrimSpace(part); len(t) > 20 {
				out = append(out, DocSection{File: e.Name(), Text: t})
			}
		}
	}
	return out
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' })
}

// SearchDocs returns the best matching lines, capped in size.
func SearchDocs(secs []DocSection, query string, n int) []map[string]string {
	q := map[string]bool{}
	for _, t := range tokens(query) {
		if len(t) >= 3 {
			q[t] = true
		}
	}
	type scored struct {
		s     DocSection
		score int
	}
	var all []scored
	for _, s := range secs {
		score := 0
		for _, t := range tokens(s.Text) {
			if q[t] {
				score++
			}
		}
		if score > 0 {
			all = append(all, scored{s, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []map[string]string
	for _, a := range all {
		if len(out) >= n {
			break
		}
		t := a.s.Text
		if len(t) > 500 {
			t = t[:500] + "..."
		}
		out = append(out, map[string]string{"file": a.s.File, "excerpt": t})
	}
	return out
}
