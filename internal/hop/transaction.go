package hop

import (
	"fmt"

	"hop.top/git/internal/output"
)

// TransactionStep represents a single step in a transaction
type TransactionStep struct {
	Name     string
	Execute  func() error
	Rollback RollbackFunc
}

// RollbackFunc is called when transaction needs to rollback
type RollbackFunc func() error

// Transaction manages multi-step operations with rollback support
type Transaction struct {
	steps     []TransactionStep
	rollbacks []RollbackFunc
	completed []int
}

// NewTransaction creates a new transaction
func NewTransaction() *Transaction {
	return &Transaction{
		steps:     []TransactionStep{},
		rollbacks: []RollbackFunc{},
		completed: []int{},
	}
}

// AddStep adds a step to the transaction
func (t *Transaction) AddStep(step TransactionStep) {
	t.steps = append(t.steps, step)
}

// Execute runs all steps, rolling back on failure
func (t *Transaction) Execute() error {
	for i, step := range t.steps {
		if err := step.Execute(); err != nil {
			t.Rollback()
			return fmt.Errorf("transaction step '%s' failed: %w", step.Name, err)
		}
		t.completed = append(t.completed, i)
		// Add rollback in reverse order (LIFO)
		if step.Rollback != nil {
			t.rollbacks = append([]RollbackFunc{step.Rollback}, t.rollbacks...)
		}
	}
	return nil
}

// Rollback undoes completed steps in reverse order
func (t *Transaction) Rollback() {
	for _, rollback := range t.rollbacks {
		if rollback != nil {
			if err := rollback(); err != nil {
				// Report and keep rolling back the remaining steps.
				output.Error("rollback failed: %v", err)
			}
		}
	}
}
