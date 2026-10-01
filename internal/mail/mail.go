// Package mail sends email over authenticated SMTP.
package mail

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"time"
)

const timeout = 30 * time.Second

type Message struct {
	To      []string
	CC      []string
	BCC     []string
	Subject string
	Text    string
	HTML    string
}

type Sender struct {
	Host     string
	Port     string
	User     string
	Password string
	FromAddr string
	FromName string
}

func (s Sender) Send(m Message) error {
	from, err := netmail.ParseAddress(s.FromAddr)
	if err != nil {
		return fmt.Errorf("invalid sender %q: %w", s.FromAddr, err)
	}
	from.Name = s.FromName

	to, err := parseAddresses(m.To)
	if err != nil {
		return err
	}
	cc, err := parseAddresses(m.CC)
	if err != nil {
		return err
	}
	bcc, err := parseAddresses(m.BCC)
	if err != nil {
		return err
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		return fmt.Errorf("no recipients")
	}
	if m.Text == "" && m.HTML == "" {
		return fmt.Errorf("empty message body")
	}

	var envelope []string
	for _, group := range [][]*netmail.Address{to, cc, bcc} {
		for _, a := range group {
			envelope = append(envelope, a.Address)
		}
	}

	c, err := s.connect()
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("sender rejected: %w", err)
	}
	for _, addr := range envelope {
		if err := c.Rcpt(addr); err != nil {
			return fmt.Errorf("recipient %s rejected: %w", addr, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(build(from, to, cc, m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s Sender) connect() (*smtp.Client, error) {
	addr := net.JoinHostPort(s.Host, s.Port)
	implicitTLS := s.Port == "465"

	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.Host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("starting smtp session: %w", err)
	}

	if !implicitTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				c.Close()
				return nil, fmt.Errorf("starting tls: %w", err)
			}
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			c.Close()
			return nil, fmt.Errorf("authenticating: %w", err)
		}
	}
	return c, nil
}

func parseAddresses(list []string) ([]*netmail.Address, error) {
	out := make([]*netmail.Address, 0, len(list))
	for _, raw := range list {
		a, err := netmail.ParseAddress(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", raw, err)
		}
		out = append(out, a)
	}
	return out, nil
}

func formatAddresses(list []*netmail.Address) string {
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.String()
	}
	return strings.Join(parts, ", ")
}

func build(from *netmail.Address, to, cc []*netmail.Address, m Message) []byte {
	var buf bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }

	header("From", from.String())
	if len(to) > 0 {
		header("To", formatAddresses(to))
	}
	if len(cc) > 0 {
		header("Cc", formatAddresses(cc))
	}
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")

	switch {
	case m.Text != "" && m.HTML != "":
		boundary := newBoundary()
		header("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", boundary))
		buf.WriteString("\r\n")
		writePart(&buf, boundary, "text/plain", m.Text)
		writePart(&buf, boundary, "text/html", m.HTML)
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	case m.HTML != "":
		header("Content-Type", "text/html; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		writeQuotedPrintable(&buf, m.HTML)
	default:
		header("Content-Type", "text/plain; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		buf.WriteString("\r\n")
		writeQuotedPrintable(&buf, m.Text)
	}
	return buf.Bytes()
}

func writePart(buf *bytes.Buffer, boundary, contentType, body string) {
	fmt.Fprintf(buf, "--%s\r\n", boundary)
	fmt.Fprintf(buf, "Content-Type: %s; charset=utf-8\r\n", contentType)
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	writeQuotedPrintable(buf, body)
	buf.WriteString("\r\n")
}

func writeQuotedPrintable(buf *bytes.Buffer, body string) {
	w := quotedprintable.NewWriter(buf)
	w.Write([]byte(body))
	w.Close()
}

func newBoundary() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "=_" + hex.EncodeToString(b)
}
