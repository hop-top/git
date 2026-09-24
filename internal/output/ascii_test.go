package output

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// assertASCII fails when s holds any byte outside 7-bit ASCII. ANSI
// colour escapes are ASCII themselves, so styled output needs no
// stripping: only the glyphs are under test.
func assertASCII(t *testing.T, what, s string) {
	t.Helper()
	for i, r := range s {
		if r > 0x7f {
			t.Errorf("%s: non-ASCII %q at byte %d in %q", what, r, i, s)
			return
		}
	}
}

// captureStdout returns what fn wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = prev }()

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	return <-done
}

// TestHumanRenderers_AreASCII renders every human-mode helper of the
// package and checks the result is plain ASCII, like git's output. Unlike
// the source scan in cmd, this also sees glyphs that never appear as a
// literal: lipgloss border presets and the charm spinner/progress frames.
func TestHumanRenderers_AreASCII(t *testing.T) {
	withMode(t, ModeHuman)
	fields := []CardField{{Key: "Target", Value: "/tmp/x"}, {Key: "Branch", Value: "feat"}}

	cases := map[string]string{
		"SuccessCard":  SuccessCard("Ready", fields),
		"WarningCard":  WarningCard("Confirm Removal", fields),
		"InfoCard":     InfoCard("Info", fields),
		"ErrorCard":    ErrorCard("Failed", fields),
		"SimpleHeader": SimpleHeader("Git-Hop System Status"),
		"Section":      Section("Configuration", []string{"a", "", "b"}),
		"TreeItem":     TreeItem(false, "api", "running"),
		"TreeItemLast": TreeItem(true, "db", ""),
		"NextStepHint": NextStepHint("git hop feat"),
		"Banner":       Banner("git hop"),
		"CompactList":  CompactList([]string{"a", "b"}, "success"),
		"Legend":       Legend(map[string]string{"active": "on disk", "missing": "gone"}),
	}
	for _, status := range []string{"success", "error", "warning", "info", "stopped", "other"} {
		cases["StatusLine/"+status] = StatusLine(status, "msg")
	}

	table := NewStatusTable("Branch", "State")
	for _, status := range []string{"success", "error", "warning", "stopped", "neutral", "other"} {
		table.AddRow(status, "b-"+status, status)
	}
	cases["StatusTable"] = table.Render()

	for name, got := range cases {
		assertASCII(t, name, got)
	}
}

// TestIcons_AreASCII covers the exported glyph set directly, including
// entries no renderer in this package uses.
func TestIcons_AreASCII(t *testing.T) {
	icons := map[string]string{
		"IconSuccess": IconSuccess, "IconError": IconError,
		"IconWarning": IconWarning, "IconRunning": IconRunning,
		"IconStopped": IconStopped, "IconClean": IconClean,
		"IconDirty": IconDirty, "IconActive": IconActive,
		"IconTreeBranch": IconTreeBranch, "IconTreeLast": IconTreeLast,
		"IconTreeLine": IconTreeLine, "IconTreeSpace": IconTreeSpace,
		"IconArrow": IconArrow, "IconArrowRight": IconArrowRight,
		"IconArrowLeft": IconArrowLeft, "IconArrowUp": IconArrowUp,
		"IconArrowDown": IconArrowDown, "IconBulletPoint": IconBulletPoint,
	}
	for name, v := range icons {
		assertASCII(t, name, v)
	}
	for i, f := range SpinnerFrames {
		assertASCII(t, "SpinnerFrames["+strconv.Itoa(i)+"]", f)
	}
}

// TestSpinnerAndProgress_AreASCII walks the spinner and progress views
// through their running and finished states.
func TestSpinnerAndProgress_AreASCII(t *testing.T) {
	withMode(t, ModeHuman)

	sp := NewSpinner("Cloning")
	m := sp.model
	for i := 0; i < len(SpinnerFrames)+1; i++ {
		assertASCII(t, "spinner running", m.View().Content)
		next, _ := m.Update(m.spinner.Tick())
		m = next.(spinnerModel)
	}
	ok, _ := m.Update(doneMsg{})
	assertASCII(t, "spinner done", ok.(spinnerModel).View().Content)
	failed, _ := m.Update(doneMsg{err: errors.New("boom")})
	assertASCII(t, "spinner failed", failed.(spinnerModel).View().Content)

	pb := NewProgressBar("Copying")
	pm := pb.model
	for _, p := range []float64{0, 0.37, 0.99} {
		next, _ := pm.Update(progressMsg(p))
		pm = next.(progressModel)
		assertASCII(t, "progress running", pm.View().Content)
	}
	next, _ := pm.Update(progressMsg(1))
	assertASCII(t, "progress done", next.(progressModel).View().Content)
}

// TestSimpleProgress_IsGitStyle pins git's progress line shape,
// "<msg>: NN% (x/y)", ending in ", done." on the last step.
func TestSimpleProgress_IsGitStyle(t *testing.T) {
	withMode(t, ModeHuman)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	SimpleProgress(1, 4, "Copying files")
	SimpleProgress(4, 4, "Copying files")
	os.Stderr = prev
	_ = w.Close()
	got, _ := io.ReadAll(r)

	want := "\rCopying files:  25% (1/4)\rCopying files: 100% (4/4), done.\n"
	if string(got) != want {
		t.Errorf("SimpleProgress = %q, want %q", got, want)
	}
}

// TestConfirmPrompts_AreASCII covers the prompts that print a warning
// title or card before asking.
func TestConfirmPrompts_AreASCII(t *testing.T) {
	withMode(t, ModeHuman)

	withStdin(t, "n\n")
	out := captureStdout(t, func() { ConfirmWithWarning("Delete everything", "details") })
	assertASCII(t, "ConfirmWithWarning", out)

	withStdin(t, "n\n")
	out = captureStdout(t, func() {
		_, _ = ConfirmDeletionAnswer("/tmp/x", []CardField{{Key: "Branch", Value: "feat"}})
	})
	assertASCII(t, "ConfirmDeletionAnswer", out)
	if !strings.Contains(out, "Confirm Removal") || !strings.Contains(out, "/tmp/x") {
		t.Errorf("deletion prompt lost its title or target: %q", out)
	}
}
