package spglobal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"supplydemand/internal/credit"
)

type Config struct {
	Enabled           bool              `json:"enabled"`
	Node              string            `json:"node"`
	Script            string            `json:"script"`
	Session           string            `json:"session"`
	Channel           string            `json:"channel"`
	Headed            bool              `json:"headed"`
	RefreshInterval   string            `json:"refreshInterval"`
	DiscoveryInterval string            `json:"discoveryInterval"`
	BatchInterval     string            `json:"batchInterval"`
	BlockedPause      string            `json:"blockedPause"`
	Timeout           string            `json:"timeout"`
	Pacing            string            `json:"pacing"`
	BatchSize         int               `json:"batchSize"`
	Entities          map[string]string `json:"entities"`
}

type Client struct {
	Config            Config
	Manual            bool
	RefreshInterval   time.Duration
	DiscoveryInterval time.Duration
	BatchInterval     time.Duration
	BlockedPause      time.Duration
	timeout           time.Duration
	pacing            time.Duration
}

func Load(path string) (*Client, error) {
	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) || path == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := Config{Node: "node", Script: "tools/sp-ratings/batch.cjs", Session: "tools/sp-ratings/.auth/session.json", Channel: "chrome", RefreshInterval: "24h", DiscoveryInterval: "168h", BatchInterval: "5m", BlockedPause: "1h", Timeout: "30s", Pacing: "2s", BatchSize: 25}
	if err := json.Unmarshal(blob, &cfg); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, nil
	}
	c := &Client{Config: cfg}
	for _, item := range []struct {
		name, value string
		target      *time.Duration
		min         time.Duration
	}{
		{"refreshInterval", cfg.RefreshInterval, &c.RefreshInterval, time.Minute},
		{"discoveryInterval", cfg.DiscoveryInterval, &c.DiscoveryInterval, time.Minute},
		{"batchInterval", cfg.BatchInterval, &c.BatchInterval, 0},
		{"blockedPause", cfg.BlockedPause, &c.BlockedPause, time.Minute},
		{"timeout", cfg.Timeout, &c.timeout, time.Second},
		{"pacing", cfg.Pacing, &c.pacing, 0},
	} {
		d, err := time.ParseDuration(item.value)
		if err != nil || d < item.min {
			return nil, fmt.Errorf("invalid S&P %s", item.name)
		}
		*item.target = d
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 100 || cfg.Node == "" || cfg.Script == "" {
		return nil, fmt.Errorf("invalid S&P batch size, node, or script")
	}
	for symbol, raw := range cfg.Entities {
		if symbol == "" || symbol != strings.ToUpper(strings.TrimSpace(symbol)) {
			return nil, fmt.Errorf("invalid S&P symbol %q", symbol)
		}
		if _, err := credit.EntityID(raw); err != nil {
			return nil, fmt.Errorf("S&P entity for %s: %w", symbol, err)
		}
	}
	return c, nil
}

func (c *Client) Ready() bool {
	if c.Manual {
		return true
	}
	if os.Getenv("SP_USERNAME") != "" && os.Getenv("SP_PASSWORD") != "" {
		return true
	}
	info, err := os.Stat(c.Config.Session)
	return err == nil && !info.IsDir() && info.Size() > 0
}

type Job struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	URL    string `json:"url,omitempty"`
}

type Result struct {
	Symbol     string             `json:"symbol"`
	Status     string             `json:"status"`
	URL        string             `json:"url"`
	Name       string             `json:"name"`
	CheckedAt  time.Time          `json:"checkedAt"`
	Data       *credit.SPData     `json:"data"`
	Error      string             `json:"error"`
	Candidates []credit.Candidate `json:"candidates"`
}

const (
	accessExitCode = 3
	maxCandidates  = 10
	stopGrace      = 15 * time.Second
)

var ErrBlocked = errors.New("S&P access blocked")

func (c *Client) Refresh(ctx context.Context, jobs []Job, apply func(Result) error) error {
	blob, err := json.Marshal(struct {
		Jobs []Job `json:"jobs"`
	}{jobs})
	if err != nil {
		return err
	}
	budget := time.Duration(len(jobs)+1) * (4*c.timeout + c.pacing)
	if c.Manual {
		budget += time.Duration(2*len(jobs)+1) * 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	args := []string{c.Config.Script, "--timeout", strconv.FormatFloat(c.timeout.Seconds(), 'f', -1, 64), "--pacing", strconv.FormatFloat(c.pacing.Seconds(), 'f', -1, 64)}
	if c.Config.Session != "" {
		args = append(args, "--session", c.Config.Session)
	}
	if c.Config.Channel != "" {
		args = append(args, "--channel", c.Config.Channel)
	}
	if c.Config.Headed {
		args = append(args, "--headed")
	}
	if c.Manual {
		args = append(args, "--manual")
	}
	cmd := exec.CommandContext(ctx, c.Config.Node, args...)
	cmd.Cancel = func() error { return stopProcess(cmd.Process) }
	cmd.WaitDelay = stopGrace
	cmd.Stdin = bytes.NewReader(blob)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if c.Manual {
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	received, readErr := readResults(stdout, jobs, apply)
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) && exit.ExitCode() == accessExitCode {
			return fmt.Errorf("%w: %s", ErrBlocked, message)
		}
		return fmt.Errorf("S&P browser batch: %w: %s", waitErr, message)
	}
	if received != len(jobs) {
		return fmt.Errorf("S&P batch returned %d of %d companies", received, len(jobs))
	}
	return nil
}

func readResults(r io.Reader, jobs []Job, apply func(Result) error) (int, error) {
	expected := map[string]Job{}
	for _, j := range jobs {
		expected[j.Symbol] = j
	}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var result Result
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			return len(seen), err
		}
		job, ok := expected[result.Symbol]
		if !ok || seen[result.Symbol] {
			return len(seen), fmt.Errorf("unexpected S&P result symbol %q", result.Symbol)
		}
		result, err := validate(job, result)
		if err != nil {
			return len(seen), err
		}
		if err := apply(result); err != nil {
			return len(seen), err
		}
		seen[result.Symbol] = true
	}
	if err := scanner.Err(); err != nil {
		return len(seen), err
	}
	return len(seen), nil
}

func validate(job Job, result Result) (Result, error) {
	switch result.Status {
	case "error", "needs_name":
		return result, nil
	case "unmatched", "ambiguous":
		result.Candidates = cleanCandidates(result.Candidates)
		return result, nil
	case "verified":
	default:
		return result, fmt.Errorf("unknown S&P discovery status %q", result.Status)
	}
	reject := func(format string, args ...any) (Result, error) {
		return Result{Symbol: result.Symbol, Status: "error", Error: fmt.Sprintf(format, args...)}, nil
	}
	if result.Data == nil {
		return reject("S&P result missing ratings")
	}
	if err := result.Data.Normalize(); err != nil {
		return reject("%v", err)
	}
	if result.URL != result.Data.SourceURL || (job.URL != "" && result.URL != job.URL) {
		return reject("S&P issuer URL mismatch")
	}
	if NormalizeName(result.Data.EntityName) != NormalizeName(job.Name) {
		return reject("S&P issuer name %q does not match %q", result.Data.EntityName, job.Name)
	}
	return result, nil
}

func cleanCandidates(in []credit.Candidate) []credit.Candidate {
	var out []credit.Candidate
	for _, c := range in {
		if c.Name == "" {
			continue
		}
		if _, err := credit.EntityID(c.URL); err != nil {
			continue
		}
		out = append(out, c)
		if len(out) == maxCandidates {
			break
		}
	}
	return out
}
