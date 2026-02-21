package hop_test

import (
	"errors"
	"testing"

	"hop.top/git/internal/hop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransaction_Success(t *testing.T) {
	executed := []string{}

	tx := hop.NewTransaction()

	tx.AddStep(hop.TransactionStep{
		Name: "step1",
		Execute: func() error {
			executed = append(executed, "step1")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback1")
			return nil
		},
	})

	tx.AddStep(hop.TransactionStep{
		Name: "step2",
		Execute: func() error {
			executed = append(executed, "step2")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback2")
			return nil
		},
	})

	err := tx.Execute()

	require.NoError(t, err)
	assert.Equal(t, []string{"step1", "step2"}, executed)
}

func TestTransaction_RollbackOnFailure(t *testing.T) {
	executed := []string{}

	tx := hop.NewTransaction()

	tx.AddStep(hop.TransactionStep{
		Name: "step1",
		Execute: func() error {
			executed = append(executed, "step1")
			return nil
		},
		Rollback: func() error {
			executed = append(executed, "rollback1")
			return nil
		},
	})

	tx.AddStep(hop.TransactionStep{
		Name: "step2",
		Execute: func() error {
			return errors.New("step2 failed")
		},
		Rollback: func() error {
			executed = append(executed, "rollback2")
			return nil
		},
	})

	err := tx.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "step2 failed")
	assert.Equal(t, []string{"step1", "rollback1"}, executed)
}
