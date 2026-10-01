package mail

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"testing"
)

func TestBuildMultipartMessage(t *testing.T) {
	from := &netmail.Address{Name: "Dash Board", Address: "from@example.com"}
	to := []*netmail.Address{{Address: "a@example.com"}}
	cc := []*netmail.Address{{Address: "b@example.com"}}

	raw := build(from, to, cc, Message{Subject: "GRAB – low end", Text: "plain é", HTML: "<b>html é</b>"})

	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("Cc"); !strings.Contains(got, "b@example.com") {
		t.Fatalf("Cc = %q", got)
	}
	if got := msg.Header.Get("Bcc"); got != "" {
		t.Fatalf("Bcc leaked into headers: %q", got)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "GRAB – low end" {
		t.Fatalf("subject = %q, err = %v", subject, err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q, err = %v", mediaType, err)
	}
	r := multipart.NewReader(msg.Body, params["boundary"])
	var bodies []string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(b))
	}
	if len(bodies) != 2 || bodies[0] != "plain é" || bodies[1] != "<b>html é</b>" {
		t.Fatalf("bodies = %q", bodies)
	}
}

func TestSendRejectsBadInput(t *testing.T) {
	s := Sender{Host: "localhost", Port: "25", FromAddr: "from@example.com"}
	if err := s.Send(Message{Text: "x"}); err == nil {
		t.Fatal("expected error for no recipients")
	}
	if err := s.Send(Message{To: []string{"not an address"}, Text: "x"}); err == nil {
		t.Fatal("expected error for invalid recipient")
	}
}
