package git

import (
	"strings"
)

// Status represents the status of a git repo
type Status struct {
	Branch string
	Clean  bool
	Files  []string
	Ahead  int
	Behind int
}

// GetStatus returns the status of the repo at dir
func (g *Git) GetStatus(dir string) (*Status, error) {
	// git status --porcelain=v2 --branch
	out, err := g.Runner.RunInDir(dir, "git", "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, err
	}

	s := &Status{
		Clean: true,
	}

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		switch parts[0] {
		case "#":
			// Header lines
			if parts[1] == "branch.head" {
				s.Branch = parts[2]
			}
			// Parse ahead/behind if needed (branch.ab)
		case "1", "2", "?":
			// Changed files
			s.Clean = false
			s.Files = append(s.Files, line)
		}
	}

	return s, nil
}
