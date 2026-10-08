package spglobal

import (
	"regexp"
	"strings"
	"unicode"
)

var nameSeparators = regexp.MustCompile(`[^a-z0-9]+`)

var nameFolder = newNameFolder()

func newNameFolder() *strings.Replacer {
	pairs := []string{"’", "", "'", "", "&", " and ", "ß", "ss", "æ", "ae", "œ", "oe", "þ", "th"}
	for letter, variants := range map[string]string{
		"a": "àáâãäåāăą",
		"c": "çćĉċč",
		"d": "ďđð",
		"e": "èéêëēĕėęě",
		"g": "ĝğġģ",
		"h": "ĥħ",
		"i": "ìíîïĩīĭį",
		"j": "ĵ",
		"k": "ķ",
		"l": "ĺļľł",
		"n": "ñńņň",
		"o": "òóôõöøōŏő",
		"r": "ŕŗř",
		"s": "śŝşš",
		"t": "ţťŧ",
		"u": "ùúûüũūŭůűų",
		"w": "ŵ",
		"y": "ýÿŷ",
		"z": "źżž",
	} {
		for _, r := range variants {
			pairs = append(pairs, string(r), letter)
		}
	}
	return strings.NewReplacer(pairs...)
}

func NormalizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, strings.ToLower(name))
	name = nameFolder.Replace(name)
	var out []string
	for _, word := range strings.Fields(nameSeparators.ReplaceAllString(name, " ")) {
		switch word {
		case "inc", "incorporated", "corp", "corporation", "co", "company", "ltd", "limited", "plc":
		default:
			out = append(out, word)
		}
	}
	if len(out) > 1 && out[0] == "the" {
		out = out[1:]
	}
	return strings.Join(out, " ")
}
