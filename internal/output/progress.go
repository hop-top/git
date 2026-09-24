package output

import (
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ProgressBar reports progress for operations with a known total. Like
// git, it prints a percentage ("<msg>:  42%") rather than drawing a bar.
type ProgressBar struct {
	program *tea.Program
	model   progressModel
}

type progressModel struct {
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
		if m.percent >= 1.0 {
			m.done = true
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	default:
		return m, nil
	}
}

func (m progressModel) View() tea.View {
	if m.done {
		return tea.NewView(m.message + ": 100%, done.")
	}

	if m.quitting {
		return tea.NewView("")
	}

	return tea.NewView(fmt.Sprintf("%s: %3.0f%%", m.message, m.percent*100))
}

// NewProgressBar creates a new progress bar with the given message
func NewProgressBar(message string) *ProgressBar {
	if CurrentMode != ModeHuman {
		return &ProgressBar{}
	}

	model := progressModel{message: message}

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

// SimpleProgress prints a git-style progress line to stderr,
// "<msg>:  NN% (x/y)", redrawn in place and closed with ", done." once
// current reaches total.
func SimpleProgress(current, total int, message string) {
	if CurrentMode != ModeHuman {
		return
	}

	percent := 100.0
	if total > 0 {
		percent = float64(current) / float64(total) * 100
	}
	fmt.Fprintf(os.Stderr, "\r%s: %3.0f%% (%d/%d)",
		message, percent, current, total)

	if current >= total {
		fmt.Fprintln(os.Stderr, ", done.")
	}
}
