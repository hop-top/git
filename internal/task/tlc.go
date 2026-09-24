package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrUnavailable reports that the tlc CLI is not installed.
var ErrUnavailable = errors.New("tlc not found on PATH")

// DefaultTimeout bounds one tlc lookup.
const DefaultTimeout = 5 * time.Second

// TLC looks tasks up with the tlc CLI (github.com/hop-top/tlc):
// `tlc task show <id> --format json`, run in the current directory so tlc
// resolves the project the way it does for the user.
type TLC struct {
	// Bin is the executable name or path; "tlc" searches PATH.
	Bin     string
	Timeout time.Duration
}

// NewTLC returns a resolver for the tlc on PATH.
func NewTLC() *TLC {
	return &TLC{Bin: "tlc", Timeout: DefaultTimeout}
}

// showOutput is the part of `tlc task show --format json` read here.
type showOutput struct {
	Task *struct {
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	} `json:"task"`
}

// Resolve returns the title and tags of task id.
func (r *TLC) Resolve(ctx context.Context, id string) (Task, error) {
	bin, err := exec.LookPath(r.Bin)
	if err != nil {
		return Task{}, ErrUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "task", "show", id, "--format", "json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// A killed tlc may leave children holding the output pipes; don't
	// wait on them past the deadline.
	cmd.WaitDelay = time.Second

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Task{}, fmt.Errorf("tlc task show %s timed out after %s", id, r.Timeout)
		}
		if msg := firstLine(stderr.String()); msg != "" {
			return Task{}, fmt.Errorf("tlc task show %s: %s", id, msg)
		}
		return Task{}, fmt.Errorf("tlc task show %s: %w", id, err)
	}

	var out showOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return Task{}, fmt.Errorf("tlc task show %s: unexpected output: %w", id, err)
	}
	if out.Task == nil || strings.TrimSpace(out.Task.Title) == "" {
		return Task{}, fmt.Errorf("tlc task show %s: no task title in output", id)
	}
	return Task{ID: id, Title: out.Task.Title, Tags: out.Task.Tags}, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
