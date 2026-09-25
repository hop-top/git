package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/kit/go/core/xdg"
)

// agentDirHint returns a slash command hint for adding the worktree directory
// to the current session when running inside a supported AI coding agent.
func agentDirHint(path string) string {
	switch {
	case os.Getenv("CLAUDE_CODE") == "1":
		return fmt.Sprintf("/add-dir %s", path)
	case os.Getenv("GEMINI_CLI") == "1":
		return fmt.Sprintf("/directory add %s", path)
	case os.Getenv("COPILOT_GH") == "true":
		return fmt.Sprintf("/add-dir %s", path)
	default:
		return ""
	}
}

// opencodeAgentHint prompts the user to choose local or global OpenCode config,
// then prints the external_directories snippet to add the worktree path.
func opencodeAgentHint(path string, in *os.File) {
	xdgConfig, err := xdg.ConfigDir("opencode")
	if err != nil {
		xdgConfig = filepath.Join(".config", "opencode")
	}
	localCfg := ".opencode/opencode.jsonc"
	globalCfg := filepath.Join(xdgConfig, "opencode.jsonc")

	fmt.Fprintf(os.Stderr, "Add %s to OpenCode config. Which?\n  1) local  (%s)\n  2) global (%s)\nChoice [1/2]: ", path, localCfg, globalCfg)

	reader := bufio.NewReader(in)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var cfgPath string
	switch choice {
	case "2":
		cfgPath = globalCfg
	default:
		cfgPath = localCfg
	}

	fmt.Fprintf(os.Stderr, "\nAdd to %s:\n\n  \"external_directories\": [\"%s\"]\n\n", cfgPath, path)
}
