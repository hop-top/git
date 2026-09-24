package config

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Git config keys of a repository's format. extensions.* are defined for
// format version 1; git honours a few of them at 0 for compatibility,
// other implementations need 1.
const (
	KeyRepositoryFormatVersion = "core.repositoryformatversion"
	KeyWorktreeConfigExtension = "extensions.worktreeConfig"
)

// WorktreeConfigAtFormatV0 reports whether the scope turns
// extensions.worktreeConfig on while core.repositoryformatversion is 0
// (unset reads as 0). Meaningful for a repository's --local scope.
func (s ConfigScope) WorktreeConfigAtFormatV0() (bool, error) {
	ext, err := s.get("--type=bool", KeyWorktreeConfigExtension)
	if err != nil || ext != "true" {
		return false, err
	}
	version, err := s.get("--type=int", KeyRepositoryFormatVersion)
	if err != nil {
		return false, err
	}
	return version == "" || version == "0", nil
}

// SetRepositoryFormatVersion1 sets core.repositoryformatversion to 1 in
// the scope.
func (s ConfigScope) SetRepositoryFormatVersion1() error {
	if _, err := s.gc.RunCmd("config", s.flag, KeyRepositoryFormatVersion, "1"); err != nil {
		return fmt.Errorf("set %s: %w", KeyRepositoryFormatVersion, err)
	}
	return nil
}

// get reads key's value in the scope, typed by typeFlag; "" when unset.
func (s ConfigScope) get(typeFlag, key string) (string, error) {
	out, err := s.gc.RunCmd("config", s.flag, typeFlag, "--get", key)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("git config %s --get %s: %w", s.flag, key, err)
	}
	return strings.TrimSpace(out), nil
}
