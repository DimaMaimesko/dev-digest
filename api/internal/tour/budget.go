package tour

import "unicode/utf8"

// callBudgetRunes is the most runes a single section's user message may
// contain (Q9): about 12k tokens. tour counts runes, not tokens, so it
// doesn't need a tokenizer.
const callBudgetRunes = 48000

// clipRunes returns the first n runes of s, and whether it cut anything.
func clipRunes(s string, n int) (string, bool) {
	count := 0
	for i := range s {
		if count == n {
			return s[:i], true
		}
		count++
	}
	return s, false
}

// clipRunesEllipsis clips s to n runes, appending "…" when it cut anything.
// The ellipsis counts against the budget, so the result never exceeds n
// runes.
func clipRunesEllipsis(s string, n int) string {
	clipped, cut := clipRunes(s, n)
	if !cut {
		return clipped
	}
	if n <= 1 {
		return "…"
	}
	body, _ := clipRunes(clipped, n-1)
	return body + "…"
}

// runeLen counts s's runes.
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}
