package sec

import (
	"testing"

	"supplydemand/internal/credit"
)

func TestHTMLToTextFeedsExtractor(t *testing.T) {
	doc := `<html><head><style>p{color:red}</style></head><body>
		<ix:header><ix:hidden>S&amp;P BB</ix:hidden></ix:header>
		<p>Our senior notes are rated <span>BBB&#8211;</span> by <b>S&amp;P</b>&nbsp;Global&nbsp;Ratings
		with a <i>stable</i> outlook.</p><script>var x = "S&P AAA";</script></body></html>`

	text := htmlToText(doc)

	got, ok := credit.Extract(text)
	if !ok {
		t.Fatalf("no rating found in %q", text)
	}
	if got.Rating != "BBB-" || got.Outlook != "Stable" {
		t.Errorf("got %+v from %q", got, text)
	}
}
