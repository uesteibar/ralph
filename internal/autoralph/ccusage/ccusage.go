package ccusage

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UsageLine represents a single usage metric parsed from ccstats output.
type UsageLine struct {
	Label      string `json:"label"`
	Percentage int    `json:"percentage"`
	ResetTime  string `json:"reset_time"`
}

// UsageGroup represents a section of usage metrics with a group label.
type UsageGroup struct {
	GroupLabel string      `json:"group_label"`
	Lines      []UsageLine `json:"lines"`
}

var usageLineRe = regexp.MustCompile(
	`^(.+?)\s+\[[\S]+\]\s+(\d+)%\s+resets in (.+)$`,
)

// Parse extracts usage groups from ccstats text output.
func Parse(output string) []UsageGroup {
	var groups []UsageGroup
	var current *UsageGroup

	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || isSeparator(line) {
			continue
		}

		if m := usageLineRe.FindStringSubmatch(line); m != nil {
			if current == nil {
				current = &UsageGroup{}
			}
			pct, _ := strconv.Atoi(m[2])
			current.Lines = append(current.Lines, UsageLine{
				Label:      strings.TrimSpace(m[1]),
				Percentage: pct,
				ResetTime:  strings.TrimSpace(m[3]),
			})
			continue
		}

		// Non-matching, non-empty line is a section header.
		if current != nil && len(current.Lines) > 0 {
			groups = append(groups, *current)
		}
		current = &UsageGroup{GroupLabel: line}
	}

	if current != nil && len(current.Lines) > 0 {
		groups = append(groups, *current)
	}
	return groups
}

func isSeparator(line string) bool {
	if len(line) == 0 {
		return false
	}
	for _, r := range line {
		if r != '─' && r != '-' && r != '=' {
			return false
		}
	}
	return true
}

// Poller periodically runs ccstats and caches parsed results.
type Poller struct {
	binary   string
	interval time.Duration
	logger   *slog.Logger

	mu      sync.RWMutex
	current []UsageGroup
}

// NewPoller creates a Poller that runs the given binary at the specified interval.
func NewPoller(binary string, interval time.Duration, logger *slog.Logger) *Poller {
	return &Poller{
		binary:   binary,
		interval: interval,
		logger:   logger,
	}
}

// Start runs ccstats immediately, then every interval. It blocks until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	if _, err := exec.LookPath(p.binary); err != nil {
		p.logger.Info("ccstats binary not found, ccusage poller disabled", "binary", p.binary)
		return
	}

	p.logger.Info("ccusage poller started", "binary", p.binary, "interval", p.interval)
	p.poll()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("ccusage poller stopped")
			return
		case <-ticker.C:
			p.poll()
		}
	}
}

// Current returns the last successfully parsed usage data, or nil.
func (p *Poller) Current() []UsageGroup {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

func (p *Poller) poll() {
	out, err := exec.Command(p.binary).Output()
	if err != nil {
		p.logger.Warn("ccstats execution failed", "error", err)
		return
	}

	groups := Parse(string(out))
	p.mu.Lock()
	p.current = groups
	p.mu.Unlock()
}
