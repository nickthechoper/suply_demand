package credit

import (
	"regexp"
	"strings"
)

const (
	contextBefore = 300
	windowLen     = 220
	outlookLen    = 300
)

var (
	agencyRe    = regexp.MustCompile(`S&P|Standard\s*(?:&|and)\s*Poor['’]?s`)
	ratingWord  = regexp.MustCompile(`(?i)\bratings?\b|\brated\b`)
	tokenRe     = regexp.MustCompile(`AAA|AA|A|BBB|BB|B|CCC|CC`)
	signedRe    = regexp.MustCompile(`^(?:AAA|AA|A|BBB|BB|B|CCC|CC)[+-]?`)
	changeRe    = regexp.MustCompile(`(?i)\b(?:from|downgrad\w*|upgrad\w*|lowered|raised)\b`)
	sentenceEnd = regexp.MustCompile(`\.\s+[A-Z]`)
	outlookRe   = regexp.MustCompile(`(?i)outlook[^.]{0,60}?\b(stable|positive|negative|developing)\b|\b(stable|positive|negative|developing)\s+outlook`)
	dashRe      = strings.NewReplacer("−", "-", "–", "-", "—", "-", "‑", "-", " ", " ")
	ratedByRe   = regexp.MustCompile(`(?i:rated|ratings? of|ratings?|assigned)\s+(?:an?\s+)?(?:(?i:\w+)\s+){0,2}?(AAA|AA[+-]?|A[+-]?|BBB[+-]?|BB[+-]?|B[+-]?|CCC[+-]?|CC)\s+(?i:by|from)\s*$`)
	bareOK      = regexp.MustCompile(`(?i)(?:rated|ratings?|of|to|from|at|and|is|was|were|:)\s*$`)
)

type candidate struct {
	rating  string
	outlook string
}

func Extract(text string) (Rating, bool) {
	text = dashRe.Replace(text)

	var found []candidate
	for _, loc := range agencyRe.FindAllStringIndex(text, -1) {
		before := text[max(0, loc[0]-contextBefore):loc[0]]
		window := text[loc[1]:min(len(text), loc[1]+outlookLen)]
		if !ratingWord.MatchString(before) && !ratingWord.MatchString(window[:min(len(window), windowLen)]) {
			continue
		}

		if m := ratedByRe.FindStringSubmatch(before); m != nil {
			found = append(found, candidate{rating: m[1], outlook: outlookNear(window, 0)})
			continue
		}

		scope := window
		if cut := sentenceEnd.FindStringIndex(scope); cut != nil {
			scope = scope[:cut[0]+1]
		}
		scope = scope[:min(len(scope), windowLen)]

		tokens := ratingTokens(scope)
		if len(tokens) == 0 {
			continue
		}
		pick := tokens[0]
		if len(tokens) > 1 && changeRe.MatchString(scope) {
			pick = tokens[len(tokens)-1]
		}
		found = append(found, candidate{rating: pick.value, outlook: outlookNear(window, pick.end)})
	}
	if len(found) == 0 {
		return Rating{}, false
	}

	counts := map[string]int{}
	best := found[0].rating
	for _, c := range found {
		counts[c.rating]++
		if counts[c.rating] > counts[best] {
			best = c.rating
		}
	}

	result := Rating{Rating: best, Rank: RankOf(best)}
	for _, c := range found {
		if c.rating == best && c.outlook != "" {
			result.Outlook = c.outlook
			break
		}
	}
	return result, true
}

type token struct {
	value string
	end   int
}

func ratingTokens(s string) []token {
	var tokens []token
	for _, loc := range tokenRe.FindAllStringIndex(s, -1) {
		start := loc[0]
		if start > 0 && isWordByte(s[start-1]) {
			continue
		}
		m := signedRe.FindString(s[start:])
		end := start + len(m)
		if end < len(s) && isWordByte(s[end]) {
			continue
		}
		signed := m[len(m)-1] == '+' || m[len(m)-1] == '-'
		if !signed && len(m) == 1 && !bareOK.MatchString(s[:start]) && strings.TrimSpace(s[:start]) != "" {
			continue
		}
		tokens = append(tokens, token{value: m, end: end})
	}
	return tokens
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func outlookNear(window string, from int) string {
	rest := window[min(from, len(window)):]
	rest = rest[:min(len(rest), outlookLen)]
	m := outlookRe.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	word := m[1]
	if word == "" {
		word = m[2]
	}
	return strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
}
