package cmd

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitcli "hop.top/kit/go/console/cli"
)

// doctorSchemaCheckEnum returns the check enum of the output schema
// doctor publishes.
func doctorSchemaCheckEnum(t *testing.T) []string {
	t.Helper()
	raw, _, ok := kitcli.GetOutputSchemaJSON(doctorCmd)
	require.True(t, ok, "doctor declares an output schema")
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	record, ok := schema.Defs["doctorRecord"]
	require.True(t, ok, "schema defines doctorRecord: %s", raw)
	return record.Properties["check"].Enum
}

// emittedDoctorChecks returns the value of every doctorCheck* constant
// declared in the package's non-test sources: every check doctor can put
// in a record.
func emittedDoctorChecks(t *testing.T) map[string]string {
	t.Helper()
	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checks := map[string]string{}
	fset := token.NewFileSet()
	for _, src := range sources {
		if strings.HasSuffix(src, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, src, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "doctorCheck") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					require.True(t, ok, "%s must be a string literal", name.Name)
					value, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					checks[name.Name] = value
				}
			}
		}
	}
	require.NotEmpty(t, checks)
	return checks
}

// Every check doctor can emit must be in the published schema, or a
// consumer validating records against it rejects real output. The
// hopspace and config checks were once missing.
func TestDoctorSchema_ListsEveryCheck(t *testing.T) {
	enum := doctorSchemaCheckEnum(t)
	for name, value := range emittedDoctorChecks(t) {
		assert.Contains(t, enum, value, "%s (%q) is missing from the schema's check enum", name, value)
	}
	assert.Len(t, enum, len(emittedDoctorChecks(t)), "the enum lists only checks doctor emits")
}
