package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"

	"github.com/spf13/afero"
)

// Writer handles writing configuration files atomically
type Writer struct {
	fs afero.Fs
}

// NewWriter creates a new configuration writer
func NewWriter(fs afero.Fs) *Writer {
	return &Writer{fs: fs}
}

// WriteOption adjusts how a write merges with the file on disk.
type WriteOption func(*writeOptions)

type writeOptions struct {
	renames map[string]string // branches key on disk -> key in the config
}

// RenamedBranch tells the write that the config's branches[newBranch] is
// the entry stored on disk as branches[oldBranch], so the members of that
// entry the config's type does not model carry over to the new key.
func RenamedBranch(oldBranch, newBranch string) WriteOption {
	return func(o *writeOptions) {
		if o.renames == nil {
			o.renames = make(map[string]string)
		}
		o.renames[oldBranch] = newBranch
	}
}

// RenamedBranches is RenamedBranch for every old -> new key of renames,
// read when the write runs: a caller that settles its renames while
// changing the config passes the map first and fills it in after.
func RenamedBranches(renames map[string]string) WriteOption {
	return func(o *writeOptions) {
		for oldBranch, newBranch := range renames {
			RenamedBranch(oldBranch, newBranch)(o)
		}
	}
}

// WriteHubConfig writes the hub configuration
func (w *Writer) WriteHubConfig(path string, config *HubConfig, opts ...WriteOption) error {
	return w.writeConfig(filepath.Join(path, "hop.json"), config, opts...)
}

// WriteHopspaceConfig writes the hopspace configuration
func (w *Writer) WriteHopspaceConfig(path string, config *HopspaceConfig, opts ...WriteOption) error {
	return w.writeConfig(filepath.Join(path, "hop.json"), config, opts...)
}

// WritePortsConfig writes the ports configuration
func (w *Writer) WritePortsConfig(path string, config *PortsConfig) error {
	return w.writeConfig(filepath.Join(path, "ports.json"), config)
}

// WriteVolumesConfig writes the volumes configuration
func (w *Writer) WriteVolumesConfig(path string, config *VolumesConfig) error {
	return w.writeConfig(filepath.Join(path, "volumes.json"), config)
}

// tempPattern names the temp file writeConfig writes next to the file
// it replaces: the * becomes random digits.
const tempPattern = "hop-config-*.tmp"

// tempName matches the names writeConfig gives its temp files, and
// nothing else.
var tempName = regexp.MustCompile(`^hop-config-[0-9]+\.tmp$`)

// IsTempName reports whether name is the name of a temp file a write of
// hop.json, ports.json or volumes.json creates next to it. A writer that
// dies before renaming it over the file leaves it behind.
func IsTempName(name string) bool {
	return tempName.MatchString(name)
}

// writeConfig writes the config to a temp file and renames it (atomic).
// Members of the existing file that config's type does not model are
// preserved (see mergeUnmodeled), following any branch renames in opts.
func (w *Writer) writeConfig(path string, config interface{}, opts ...WriteOption) error {
	var o writeOptions
	for _, opt := range opts {
		opt(&o)
	}
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if prev, err := afero.ReadFile(w.fs, path); err == nil {
		prev = renameBranchKeys(prev, o.renames)
		data = mergeUnmodeled(reflect.TypeOf(config), data, prev)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, data, "", "  "); err != nil {
		return fmt.Errorf("failed to format config: %w", err)
	}
	data = indented.Bytes()

	dir := filepath.Dir(path)
	if err := w.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmpFile, err := afero.TempFile(w.fs, dir, tempPattern)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		if _, err := w.fs.Stat(tmpPath); err == nil {
			w.fs.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := w.fs.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}

	return nil
}
