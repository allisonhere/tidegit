package diff

import "unicode"

// maxWordTokens bounds the word-level diff so a pathological line cannot make
// the parser quadratic. Longer lines are treated as wholly changed.
const maxWordTokens = 400

type token struct {
	text       string
	start, end int // rune offsets
}

// wordSpans diffs two lines at token granularity and returns changed spans for
// each. Unpaired or over-long lines are marked entirely changed.
func wordSpans(oldText, newText string) ([]Span, []Span) {
	oldTokens := tokenize(oldText)
	newTokens := tokenize(newText)
	if len(oldTokens) > maxWordTokens || len(newTokens) > maxWordTokens {
		return wholeLineSpans(oldText), wholeLineSpans(newText)
	}
	commonOld, commonNew := lcsTokens(oldTokens, newTokens)
	return changedSpans(oldTokens, commonOld), changedSpans(newTokens, commonNew)
}

// tokenize splits text into words, individual punctuation and whitespace runs,
// each remembering its rune range.
func tokenize(text string) []token {
	runes := []rune(text)
	var out []token
	i := 0
	for i < len(runes) {
		start := i
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			for i < len(runes) && unicode.IsSpace(runes[i]) {
				i++
			}
		case isWordRune(r):
			for i < len(runes) && isWordRune(runes[i]) {
				i++
			}
		default:
			i++
		}
		out = append(out, token{text: string(runes[start:i]), start: start, end: i})
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// lcsTokens computes a longest common subsequence and returns a boolean mask
// per input marking membership. The backtracking marks both sides so each line
// gets its own changed spans.
func lcsTokens(a, b []token) ([]bool, []bool) {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i].text == b[j].text {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	commonA := make([]bool, n)
	commonB := make([]bool, m)
	i, j := 0, 0
	for i < n && j < m {
		if a[i].text == b[j].text {
			commonA[i] = true
			commonB[j] = true
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return commonA, commonB
}

// changedSpans converts the common mask into merged changed spans.
func changedSpans(tokens []token, common []bool) []Span {
	var spans []Span
	start := -1
	for i, t := range tokens {
		if common[i] {
			if start >= 0 {
				spans = append(spans, Span{Start: start, End: tokens[i-1].end, Changed: true})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = t.start
		}
	}
	if start >= 0 {
		spans = append(spans, Span{Start: start, End: tokens[len(tokens)-1].end, Changed: true})
	}
	return spans
}
