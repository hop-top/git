package output

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Spinner represents a CLI spinner for long-running operations
type Spinner struct {
	program *tea.Program
	model   spinnerModel
}

type spinnerModel struct {
	spinner  spinner.Model
	message  string
	done     bool
	err      error
	quitting bool
}

type doneMsg struct {
	err error
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case doneMsg:
		m.done = true
		m.err = msg.err
		m.quitting = true
		return m, tea.Quit

	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m spinnerModel) View() string {
	if m.done {
		if m.err != nil {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗ " + m.message + ": " + m.err.Error())
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓ " + m.message)
	}

	if m.quitting {
		return ""
	}

	return fmt.Sprintf("%s %s", m.spinner.View(), m.message)
}

// NewSpinner creates a new spinner with the given message
func NewSpinner(message string) *Spinner {
	if CurrentMode != ModeHuman {
		return &Spinner{}
	}

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	model := spinnerModel{
		spinner: s,
		message: message,
	}

	return &Spinner{
		program: tea.NewProgram(model, tea.WithOutput(os.Stderr)),
		model:   model,
	}
}

// Start begins the spinner animation
func (s *Spinner) Start() {
	if s.program == nil {
		return
	}

	go func() {
		if _, err := s.program.Run(); err != nil {
			Error("Error running spinner: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
}

// Stop stops the spinner with success
func (s *Spinner) Stop() {
	if s.program == nil {
		return
	}

	s.program.Send(doneMsg{err: nil})
	time.Sleep(100 * time.Millisecond)
}

// StopWithError stops the spinner with an error
func (s *Spinner) StopWithError(err error) {
	if s.program == nil {
		return
	}

	s.program.Send(doneMsg{err: err})
	time.Sleep(100 * time.Millisecond)
}

// UpdateMessage updates the spinner message
func (s *Spinner) UpdateMessage(message string) {
	s.model.message = message
}

// WithSpinner runs a function with a spinner
func WithSpinner(message string, fn func() error) error {
	if CurrentMode != ModeHuman {
		return fn()
	}

	s := NewSpinner(message)
	s.Start()

	err := fn()

	if err != nil {
		s.StopWithError(err)
	} else {
		s.Stop()
	}

	return err
}

// ProgressWriter wraps an io.Writer to track progress
type ProgressWriter struct {
	writer   io.Writer
	total    int64
	written  int64
	onUpdate func(written, total int64)
}

// NewProgressWriter creates a new progress tracking writer
func NewProgressWriter(w io.Writer, total int64, onUpdate func(written, total int64)) *ProgressWriter {
	return &ProgressWriter{
		writer:   w,
		total:    total,
		onUpdate: onUpdate,
	}
}

// Write implements io.Writer
func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	pw.written += int64(n)

	if pw.onUpdate != nil {
		pw.onUpdate(pw.written, pw.total)
	}

	return n, err
}
