package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"hop.top/kit/go/core/xdg"
	"hop.top/kit/go/runtime/bus"

	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
	"hop.top/git/internal/output"
)

// eventSinkTimeout caps how long one event write may hold up a command.
// A local append takes well under a millisecond; the cap only matters when
// the sink path sits on a stalled mount.
const eventSinkTimeout = 250 * time.Millisecond

// attachEventSinks tees EventBus into the sink configured by
// hop.events.sink / hop.events.path, so lifecycle events reach external
// consumers without file hooks.
//
// Nothing is attached under --dry-run: a preview must leave no trace, and
// an event would announce a change that never happened. Sink failures,
// including misconfiguration, are side-channel problems: they are reported
// on stderr under --verbose and never affect the command's outcome.
func attachEventSinks(cmd *cobra.Command) {
	if commandIsDryRun(cmd) {
		return
	}
	path, err := resolveEventSinkPath(config.NewGitConfig(), os.Getenv)
	if err != nil {
		reportEventSinkError(fmt.Errorf("event sink: %w", err))
		return
	}
	if path == "" {
		return
	}
	EventBus = withEventSink(EventBus, path, reportEventSinkError)
}

// resolveEventSinkPath returns the JSONL file events should be appended
// to, or "" when no git-config sink applies.
//
// kit's own KIT_BUS_SINK env switch is honored by bus.New itself; when it
// is set, env wins over git config (env > config, as everywhere else) and
// no second sink is attached, so events are not written twice.
func resolveEventSinkPath(gc *config.GitConfig, getenv func(string) string) (string, error) {
	if strings.TrimSpace(getenv(bus.EnvSinkKind)) != "" {
		return "", nil
	}

	kind, err := gc.GetString(config.KeyEventsSink)
	if errors.Is(err, config.ErrKeyNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "none":
		return "", nil
	case "jsonl":
	default:
		return "", fmt.Errorf("unknown %s %q (want jsonl or none); events not recorded", config.KeyEventsSink, kind)
	}

	path, err := gc.GetPath(config.KeyEventsPath)
	if err == nil && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if err != nil && !errors.Is(err, config.ErrKeyNotFound) {
		return "", err
	}
	dir, err := xdg.StateDir("git-hop")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.jsonl"), nil
}

// withEventSink wraps b so every accepted publish is also appended to path.
// Subscriptions still go to b, so in-process subscribers are unaffected.
func withEventSink(b bus.Bus, path string, onErr bus.ErrFunc) bus.Bus {
	return bus.NewTeeBus(b, []bus.Sink{events.NewFileSink(path, eventSinkTimeout)}, onErr)
}

// commandIsDryRun reports whether the running command's --dry-run is set,
// whether it resolved to the global flag or a command-local one.
func commandIsDryRun(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("dry-run")
	return f != nil && f.Value.String() == "true"
}

func reportEventSinkError(err error) {
	if verboseEnabled() {
		output.Warn("%v", err)
	}
}
