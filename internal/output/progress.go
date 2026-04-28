package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"hop.top/kit/tui"
)

// ProgressBar represents a progress bar for operations with known total
type ProgressBar struct {
	program *tea.Program
	model   progressModel
}

type progressModel struct {
	progress tui.Progress
	message  string
	percent  float64
	done     bool
	quitting bool
}

type progressMsg float64

func (m progressModel) Init() tea.Cmd {
	return nil
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case progressMsg:
		m.percent = float64(msg)
		m.progress = m.progress.SetPercent(m.percent)
		if m.percent >= 1.0 {
			m.done = true
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case tea.WindowSizeMsg:
		w := msg.Width - 4
		if w > 80 {
			w = 80
		}
		m.progress = m.progress.SetWidth(w)
		return m, nil

	default:
		return m, nil
	}
}

func (m progressModel) View() tea.View {
	if m.done {
		s := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Render("✓ " + m.message + " (100%)")
		return tea.NewView(s)
	}

	if m.quitting {
		return tea.NewView("")
	}

	percent := fmt.Sprintf(" %.0f%%", m.percent*100)
	return tea.NewView(
		m.message + "\n" + m.progress.View() + percent,
	)
}

// NewProgressBar creates a new progress bar with the given message
func NewProgressBar(message string) *ProgressBar {
	if CurrentMode != ModeHuman {
		return &ProgressBar{}
	}

	prog := tui.NewProgress(theme).SetWidth(80)

	model := progressModel{
		progress: prog,
		message:  message,
		percent:  0,
	}

	return &ProgressBar{
		program: tea.NewProgram(model, tea.WithOutput(os.Stderr)),
		model:   model,
	}
}

// Start begins the progress bar display
func (pb *ProgressBar) Start() {
	if pb.program == nil {
		return
	}

	go func() {
		if _, err := pb.program.Run(); err != nil {
			Error("Error running progress bar: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
}

// Update updates the progress (0.0 to 1.0)
func (pb *ProgressBar) Update(percent float64) {
	if pb.program == nil {
		return
	}

	if percent < 0 {
		percent = 0
	}
	if percent > 1.0 {
		percent = 1.0
	}

	pb.program.Send(progressMsg(percent))
}

// Increment increments the progress by a delta
func (pb *ProgressBar) Increment(delta float64) {
	if pb.program == nil {
		return
	}

	pb.model.percent += delta
	pb.Update(pb.model.percent)
}

// Finish completes the progress bar
func (pb *ProgressBar) Finish() {
	if pb.program == nil {
		return
	}

	pb.Update(1.0)
	time.Sleep(200 * time.Millisecond)
}

// MultiStepProgress tracks progress across multiple steps
type MultiStepProgress struct {
	steps       []string
	current     int
	totalSteps  int
	showSpinner bool
	spinner     *Spinner
}

// NewMultiStepProgress creates a progress tracker for multiple steps
func NewMultiStepProgress(steps []string) *MultiStepProgress {
	return &MultiStepProgress{
		steps:       steps,
		current:     0,
		totalSteps:  len(steps),
		showSpinner: CurrentMode == ModeHuman,
	}
}

// Start begins the multi-step progress
func (msp *MultiStepProgress) Start() {
	if !msp.showSpinner {
		return
	}

	if msp.current < len(msp.steps) {
		msg := fmt.Sprintf(
			"[%d/%d] %s",
			msp.current+1, msp.totalSteps,
			msp.steps[msp.current],
		)
		msp.spinner = NewSpinner(msg)
		msp.spinner.Start()
	}
}

// Next moves to the next step
func (msp *MultiStepProgress) Next() {
	if !msp.showSpinner {
		msp.current++
		return
	}

	if msp.spinner != nil {
		msp.spinner.Stop()
	}

	msp.current++

	if msp.current < len(msp.steps) {
		msg := fmt.Sprintf(
			"[%d/%d] %s",
			msp.current+1, msp.totalSteps,
			msp.steps[msp.current],
		)
		msp.spinner = NewSpinner(msg)
		msp.spinner.Start()
	}
}

// Fail marks the current step as failed
func (msp *MultiStepProgress) Fail(err error) {
	if !msp.showSpinner {
		return
	}

	if msp.spinner != nil {
		msp.spinner.StopWithError(err)
	}
}

// Finish completes all steps
func (msp *MultiStepProgress) Finish() {
	if !msp.showSpinner {
		return
	}

	if msp.spinner != nil {
		msp.spinner.Stop()
	}
}

// SimpleProgress shows a simple percentage-based progress
func SimpleProgress(current, total int, message string) {
	if CurrentMode != ModeHuman {
		return
	}

	percent := float64(current) / float64(total) * 100
	bar := progressBar(current, total, 40)
	fmt.Fprintf(
		os.Stderr,
		"\r%s %s %.0f%% (%d/%d)",
		message, bar, percent, current, total,
	)

	if current >= total {
		fmt.Fprintln(os.Stderr)
	}
}

func progressBar(current, total, width int) string {
	if total == 0 {
		return strings.Repeat("━", width)
	}

	filled := int(float64(current) / float64(total) * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("━", filled) +
		strings.Repeat("─", width-filled)
	return lipgloss.NewStyle().
		Foreground(ColorAccent).
		Render(bar)
}
