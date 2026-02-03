package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProgressBar represents a progress bar for operations with known total
type ProgressBar struct {
	program *tea.Program
	model   progressModel
}

type progressModel struct {
	progress progress.Model
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
	case tea.KeyMsg:
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

	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - 4
		if m.progress.Width > 80 {
			m.progress.Width = 80
		}
		return m, nil

	default:
		return m, nil
	}
}

func (m progressModel) View() string {
	if m.done {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ " + m.message + " (100%)")
	}

	if m.quitting {
		return ""
	}

	percent := fmt.Sprintf(" %.0f%%", m.percent*100)
	return m.message + "\n" + m.progress.ViewAs(m.percent) + percent
}

// NewProgressBar creates a new progress bar with the given message
func NewProgressBar(message string) *ProgressBar {
	if CurrentMode != ModeHuman {
		return &ProgressBar{}
	}

	prog := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(80),
		progress.WithoutPercentage(),
	)

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
			fmt.Fprintf(os.Stderr, "Error running progress bar: %v\n", err)
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
		msg := fmt.Sprintf("[%d/%d] %s", msp.current+1, msp.totalSteps, msp.steps[msp.current])
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
		msg := fmt.Sprintf("[%d/%d] %s", msp.current+1, msp.totalSteps, msp.steps[msp.current])
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
	fmt.Fprintf(os.Stderr, "\r%s %s %.0f%% (%d/%d)", message, bar, percent, current, total)

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

	bar := strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(bar)
}
