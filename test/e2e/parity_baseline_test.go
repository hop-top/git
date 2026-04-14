package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func assertNoANSI(t *testing.T, output string) {
	t.Helper()
	if m := ansiRE.FindString(output); m != "" {
		t.Errorf("stdout contains ANSI escape code: %q", m)
	}
}

type parityEnv struct {
	binPath string
	rootDir string
	envVars []string
}

func setupParityEnv(t *testing.T) *parityEnv {
	t.Helper()
	rootDir := CreateTempDir(t)
	t.Cleanup(func() { os.RemoveAll(rootDir) })

	binPath := filepath.Join(rootDir, "git-hop")
	projectRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if filepath.Base(projectRoot) == "e2e" {
		projectRoot = filepath.Dir(filepath.Dir(projectRoot))
	}
	RunCommand(t, projectRoot, "go", "build", "-buildvcs=false", "-o", binPath, "main.go")

	gitConfigPath := filepath.Join(rootDir, "gitconfig")
	WriteFile(t, gitConfigPath, "[user]\n\tname = Test\n\temail = t@t.com\n[init]\n\tdefaultBranch = main\n")

	envVars := []string{
		"HOME=" + rootDir,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_GLOBAL=" + gitConfigPath,
		"XDG_CONFIG_HOME=" + filepath.Join(rootDir, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(rootDir, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(rootDir, ".local", "state"),
		"GIT_HOP_DATA_HOME=" + filepath.Join(rootDir, "data"),
		"NO_COLOR=1",
	}

	return &parityEnv{binPath: binPath, rootDir: rootDir, envVars: envVars}
}

func (p *parityEnv) run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(p.binPath, args...)
	cmd.Dir = p.rootDir
	cmd.Env = p.envVars
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("exec error: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func (p *parityEnv) runNoColor(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	origEnv := p.envVars
	p.envVars = append([]string{}, origEnv...)
	found := false
	for i, v := range p.envVars {
		if strings.HasPrefix(v, "NO_COLOR=") {
			p.envVars[i] = "NO_COLOR=1"
			found = true
			break
		}
	}
	if !found {
		p.envVars = append(p.envVars, "NO_COLOR=1")
	}
	defer func() { p.envVars = origEnv }()
	return p.run(t, args...)
}

func (p *parityEnv) runWithColor(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	origEnv := p.envVars
	var filtered []string
	for _, v := range origEnv {
		if !strings.HasPrefix(v, "NO_COLOR=") {
			filtered = append(filtered, v)
		}
	}
	p.envVars = filtered
	defer func() { p.envVars = origEnv }()
	return p.run(t, args...)
}

func goldenPath(parts ...string) string {
	elems := append([]string{"fixtures", "parity"}, parts...)
	return filepath.Join(elems...)
}

func updateGolden(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir golden dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func assertGolden(t *testing.T, path, actual string) {
	t.Helper()
	golden, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("Golden file missing; creating: %s", path)
			updateGolden(t, path, actual)
			return
		}
		t.Fatalf("read golden: %v", err)
	}
	if string(golden) != actual {
		t.Errorf("output differs from golden %s\n--- golden ---\n%s\n--- actual ---\n%s",
			path, string(golden), actual)
	}
}

// TestParityHelp records --help output for every subcommand and
// verifies stdout contains no ANSI escape codes.
func TestParityHelp(t *testing.T) {
	env := setupParityEnv(t)

	subcommands := []string{
		"", // root (no subcommand)
		"add",
		"completion",
		"doctor",
		"env",
		"init",
		"list",
		"merge",
		"move",
		"prune",
		"remove",
		"status",
		"upgrade",
	}

	for _, sub := range subcommands {
		name := sub
		if name == "" {
			name = "root"
		}
		t.Run(name+"/help", func(t *testing.T) {
			var args []string
			if sub != "" {
				args = []string{sub, "--help"}
			} else {
				args = []string{"--help"}
			}

			stdout, stderr, exitCode := env.run(t, args...)

			// Help text goes to stderr (cobra default when called
			// via --help on the root or via subcommand --help).
			// Some commands put it on stdout. Accept either location
			// but capture the "help text" wherever it lands.
			helpText := stdout
			if helpText == "" {
				helpText = stderr
			}

			if exitCode != 0 {
				t.Errorf("--help exit code: got %d, want 0", exitCode)
			}

			assertNoANSI(t, stdout)

			gp := goldenPath("help", name+".golden")
			assertGolden(t, gp, helpText)
		})
	}
}

// TestParityHelpStdoutCleanWithColor verifies stdout stays
// ANSI-free even when NO_COLOR is NOT set.
func TestParityHelpStdoutCleanWithColor(t *testing.T) {
	env := setupParityEnv(t)

	subcommands := []string{"", "add", "list", "status"}
	for _, sub := range subcommands {
		name := sub
		if name == "" {
			name = "root"
		}
		t.Run(name, func(t *testing.T) {
			var args []string
			if sub != "" {
				args = []string{sub, "--help"}
			} else {
				args = []string{"--help"}
			}
			stdout, _, _ := env.runWithColor(t, args...)
			assertNoANSI(t, stdout)
		})
	}
}

// TestParityExitCodes verifies exit codes for known scenarios.
func TestParityExitCodes(t *testing.T) {
	env := setupParityEnv(t)

	t.Run("root_no_args", func(t *testing.T) {
		_, _, code := env.run(t)
		if code != 0 {
			t.Errorf("root no-args exit code: got %d, want 0", code)
		}
	})

	t.Run("help_flag", func(t *testing.T) {
		_, _, code := env.run(t, "--help")
		if code != 0 {
			t.Errorf("--help exit code: got %d, want 0", code)
		}
	})

	t.Run("version_flag", func(t *testing.T) {
		_, _, code := env.run(t, "--version")
		if code != 0 {
			t.Errorf("--version exit code: got %d, want 0", code)
		}
	})

	t.Run("unknown_flag", func(t *testing.T) {
		_, _, code := env.run(t, "--nonexistent-flag-xyz")
		if code == 0 {
			t.Errorf("unknown flag exit code: got 0, want non-zero")
		}
	})

	t.Run("unknown_subcommand_flag", func(t *testing.T) {
		_, _, code := env.run(t, "list", "--nonexistent-flag-xyz")
		if code == 0 {
			t.Errorf("unknown subcommand flag exit code: got 0, want non-zero")
		}
	})
}

// TestParityEnvSubcommands verifies env subcommand --help output and
// exit codes.
func TestParityEnvSubcommands(t *testing.T) {
	env := setupParityEnv(t)

	subs := []string{"gc", "generate", "start", "stop"}
	for _, sub := range subs {
		t.Run(sub+"/help", func(t *testing.T) {
			stdout, stderr, code := env.run(t, "env", sub, "--help")
			helpText := stdout
			if helpText == "" {
				helpText = stderr
			}
			if code != 0 {
				t.Errorf("env %s --help exit code: got %d, want 0", sub, code)
			}
			assertNoANSI(t, stdout)
			gp := goldenPath("help", "env_"+sub+".golden")
			assertGolden(t, gp, helpText)
		})
	}
}

// TestParityUpgradeSubcommands verifies upgrade subcommand help.
func TestParityUpgradeSubcommands(t *testing.T) {
	env := setupParityEnv(t)

	t.Run("preamble/help", func(t *testing.T) {
		stdout, stderr, code := env.run(t, "upgrade", "preamble", "--help")
		helpText := stdout
		if helpText == "" {
			helpText = stderr
		}
		if code != 0 {
			t.Errorf("upgrade preamble --help exit code: got %d, want 0", code)
		}
		assertNoANSI(t, stdout)
		gp := goldenPath("help", "upgrade_preamble.golden")
		assertGolden(t, gp, helpText)
	})
}
