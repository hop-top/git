package hop

import (
	"fmt"
	"strings"
	"time"

	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

type StashRef struct {
	Index     int       `json:"index"`
	Message   string    `json:"message"`
	SHA       string    `json:"sha"`
	Timestamp time.Time `json:"timestamp"`
}

type StashManager struct {
	git git.GitInterface
	fs  afero.Fs
}

func NewStashManager(g git.GitInterface, fs afero.Fs) *StashManager {
	return &StashManager{
		git: g,
		fs:  fs,
	}
}

func (s *StashManager) ExportStashes(repoPath string) ([]StashRef, error) {
	out, err := s.git.RunInDir(repoPath, "git", "stash", "list")
	if err != nil {
		return nil, fmt.Errorf("failed to list stashes: %w", err)
	}

	lines := strings.Split(out, "\n")
	var stashes []StashRef

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var index int
		fmt.Sscanf(line, "stash@{%d}", &index)

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		message := strings.TrimSpace(parts[1])

		sha, err := s.git.RunInDir(repoPath, "git", "rev-parse", fmt.Sprintf("stash@{%d}", index))
		if err != nil {
			continue
		}

		timestampStr, err := s.git.RunInDir(repoPath, "git", "log", "-1", "--format=%ci", fmt.Sprintf("stash@{%d}", index))
		if err != nil {
			timestampStr = ""
		}

		parsedTime, _ := time.Parse("2006-01-02 15:04:05 -0700", timestampStr)

		stashes = append(stashes, StashRef{
			Index:     index,
			Message:   message,
			SHA:       strings.TrimSpace(sha),
			Timestamp: parsedTime,
		})
	}

	return stashes, nil
}

func (s *StashManager) ImportStashes(repoPath string, stashes []StashRef) error {
	for _, stash := range stashes {
		sha, err := s.git.RunInDir(repoPath, "git", "rev-parse", fmt.Sprintf("stash@{%d}", stash.Index))
		if err != nil {
			continue
		}

		if strings.TrimSpace(sha) == stash.SHA {
			fmt.Printf("Verified stash@{%d}: %s\n", stash.Index, stash.Message)
		}
	}

	return nil
}

func (s *StashManager) ListStashes(repoPath string) ([]StashRef, error) {
	return s.ExportStashes(repoPath)
}

func (s *StashManager) GetStashCount(repoPath string) (int, error) {
	stashes, err := s.ExportStashes(repoPath)
	if err != nil {
		return 0, err
	}
	return len(stashes), nil
}
