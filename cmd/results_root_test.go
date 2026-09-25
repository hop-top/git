package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"hop.top/git/internal/cli"
)

// The root command publishes one result schema for switch, clone and
// fork-attach, and its action enum is exactly the actions the root
// reports: a consumer validating results against the schema accepts every
// real one and no action the root never emits.
func TestRootSchema_ListsEveryAction(t *testing.T) {
	enum := schemaEnum(t, cli.RootCmd, "RootResult", "action")
	actions := prefixedConstantsIn(t, filepath.Join("..", "internal", "cli"), "rootAction")
	for name, value := range actions {
		assert.Contains(t, enum, value, "%s (%q) is missing from the schema's action enum", name, value)
	}
	assert.Len(t, enum, len(actions), "the enum lists only actions the root reports")
}
