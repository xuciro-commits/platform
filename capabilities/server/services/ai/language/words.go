package language

import (
	"strings"
	"unicode"
)

func TokenizeWords(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 1 || len(cur) == 1 && unicode.IsDigit(cur[0]) {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}
