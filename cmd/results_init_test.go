package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// init publishes its result schema, and the schema's action enum is
// exactly the actions init reports.
func TestInitSchema_ListsEveryAction(t *testing.T) {
	enum := schemaEnum(t, initCmd, "initResult", "action")
	actions := prefixedConstants(t, "initAction")
	for name, value := range actions {
		assert.Contains(t, enum, value, "%s (%q) is missing from the schema's action enum", name, value)
	}
	assert.Len(t, enum, len(actions), "the enum lists only actions init reports")
}
