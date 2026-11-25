// Ref: .teos/sections/04 — palette fuzzy search (<100 ms): subsequence
// scoring over id/name/application/tags, context-matched entries first.
package registry

import (
	"sort"
	"strings"
)

// Scored is one ranked search result.
type Scored struct {
	*Manifest
	Score   int  `json:"-"`
	Context bool `json:"context,omitempty"` // matched the session's live context
}

// Search ranks the resolved index: context-matching entries first, then
// fuzzy score descending (§04 "context match, then frequency" — the
// frequency signal lands when usage stats exist; score is the stable
// second key).
func (r *Registry) Search(q string, contextIDs map[string]bool, limit int) []Scored {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []Scored
	for _, m := range r.List() {
		sc := score(q, m)
		if q != "" && sc < 0 {
			continue
		}
		out = append(out, Scored{Manifest: m, Score: sc, Context: contextIDs[m.ID]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// score is a subsequence match: every query char must appear in order
// across the haystack (id + name + application + tags). Consecutive runs
// and word-boundary hits score higher; no match → -1. An empty query
// matches everything at score 0 (the unfiltered palette view).
func score(q string, m *Manifest) int {
	if q == "" {
		return 0
	}
	hay := []rune(strings.ToLower(m.ID + " " + m.Name + " " + m.Application + " " + strings.Join(m.Tags, " ")))
	hi := 0
	sc := 0
	run := 0
	for _, qc := range q {
		found := -1
		for hi < len(hay) {
			if hay[hi] == qc {
				found = hi
				break
			}
			hi++
		}
		if found < 0 {
			return -1
		}
		run++
		sc += 1 + run*2 // consecutive hits compound
		if found == 0 || hay[found-1] == ' ' || hay[found-1] == '.' || hay[found-1] == '-' {
			sc += 3 // boundary match
		}
		hi = found + 1
	}
	// Shorter haystacks win ties — "git.status" over "git.statusline.fmt".
	sc -= len(hay) / 64
	return sc
}
