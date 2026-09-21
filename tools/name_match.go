package tools

import (
	"sort"
	"strings"
)

// foldName lowercases s and strips the diacritics that appear in SL stop
// names so "Årstaberg", "arstaberg" and "ÅRSTABERG" compare equal.
func foldName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case 'å', 'ä':
			b.WriteRune('a')
		case 'ö':
			b.WriteRune('o')
		case 'é', 'è', 'ë':
			b.WriteRune('e')
		case 'ü':
			b.WriteRune('u')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nameMatchesQuery reports whether a candidate name is the query itself,
// ignoring case, surrounding whitespace and diacritics.
func nameMatchesQuery(name, query string) bool {
	return query != "" && foldName(name) == foldName(query)
}

// rankByQuality sorts stop-finder results by matchQuality (highest first)
// and, within a tie, puts an exact-name match for the query first. Upstream
// does not guarantee quality ordering — a "Nockeby" query returns
// [Nockeby (på Drottningholmsvägen) 1000, Nockebyhov 977, Nockeby 1000] —
// and the stable sort keeps upstream order for everything else.
func rankByQuality[T any](xs []T, query string, quality func(T) int, name func(T) string) {
	sort.SliceStable(xs, func(i, j int) bool {
		qi, qj := quality(xs[i]), quality(xs[j])
		if qi != qj {
			return qi > qj
		}
		return nameMatchesQuery(name(xs[i]), query) && !nameMatchesQuery(name(xs[j]), query)
	})
}

func rankCandidates(cands []locationCandidate, query string) {
	rankByQuality(cands, query,
		func(c locationCandidate) int { return c.MatchQuality },
		func(c locationCandidate) string { return c.Name })
}
