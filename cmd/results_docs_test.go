package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	kitcli "hop.top/kit/go/console/cli"
	kitout "hop.top/kit/go/console/output"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/output"
)

// The result shapes the reference and the agent cheatsheet document are
// checked against the shapes the commands declare, so a new command, a
// renamed or added field, a changed column or a new enum value fails
// here instead of shipping as stale docs.

// declaredShape is a command's declared result: its Go type and schema.
type declaredShape struct {
	path   string // command path without the root's name; "" for the root
	typ    reflect.Type
	schema json.RawMessage
}

// declaredShapes lists every command that declares a result shape.
func declaredShapes(t *testing.T) map[string]declaredShape {
	t.Helper()
	out := map[string]declaredShape{}
	for path, c := range commandTree(cli.RootCmd) {
		shape, ok := output.ResultShape(c)
		if !ok {
			continue
		}
		raw, _, ok := kitcli.GetOutputSchemaJSON(c)
		if !ok {
			t.Fatalf("'git hop %s' registers a result shape but no output schema", path)
		}
		out[path] = declaredShape{path: path, typ: reflect.TypeOf(shape), schema: raw}
	}
	return out
}

// elem is the struct a shape renders: the shape itself, or a list's
// element.
func (d declaredShape) elem() reflect.Type {
	t := d.typ
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t
}

// kind is "list" for a list of records, "object" for one object.
func (d declaredShape) kind() string {
	t := d.typ
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		return "list"
	}
	return "object"
}

// columns is the columnar layout (csv, text, --porcelain), space-joined.
func (d declaredShape) columns() string {
	return strings.Join(kitout.TableHeaders(d.typ), " ")
}

// fields are the top-level json keys of a record; required are those
// without omitempty, present in every record.
func (d declaredShape) fields() (all, required []string) {
	t := d.elem()
	for i := range t.NumField() {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		all = append(all, name)
		if !strings.Contains(opts, "omitempty") {
			required = append(required, name)
		}
	}
	return all, required
}

// enums are every enum value the schema declares, in any definition.
func (d declaredShape) enums(t *testing.T) []string {
	t.Helper()
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(d.schema, &schema); err != nil {
		t.Fatalf("'git hop %s' schema: %v", d.path, err)
	}
	var out []string
	for _, def := range schema.Defs {
		for _, p := range def.Properties {
			out = append(out, p.Enum...)
		}
	}
	return out
}

// decodes reports whether doc is exactly this shape: every key a field
// of it (at any depth), and every required field in every record.
func (d declaredShape) decodes(doc []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(reflect.New(d.typ.Elem()).Interface()); err != nil {
		return false
	}
	var records []map[string]any
	if d.kind() == "list" {
		if json.Unmarshal(doc, &records) != nil {
			return false
		}
	} else {
		var one map[string]any
		if json.Unmarshal(doc, &one) != nil {
			return false
		}
		records = []map[string]any{one}
	}
	_, required := d.fields()
	for _, r := range records {
		for _, f := range required {
			if _, ok := r[f]; !ok {
				return false
			}
		}
	}
	return len(records) > 0
}

// documentedCommand maps a documented command ("git hop env start",
// "git hop <branch>") to its path: "env start", or "" for the root.
func documentedCommand(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "git hop"))
	if s == "" || strings.HasPrefix(s, "<") {
		return ""
	}
	return s
}

// shapeRow is one row of a documented result-shape table.
type shapeRow struct {
	kind, extra string // extra: the third cell
}

var (
	backticked = regexp.MustCompile("`([^`]+)`")
	mdLink     = regexp.MustCompile(`\(#([a-z0-9-]+)\)`)
)

// parseShapeTable reads the table that follows heading in doc: the
// command cell (one or more backticked commands), the result kind, and
// the third cell, by command path.
func parseShapeTable(t *testing.T, doc, heading string) map[string]shapeRow {
	t.Helper()
	i := strings.Index(doc, "\n"+heading+"\n")
	if i < 0 {
		t.Fatalf("heading %q not found", heading)
	}
	rows := map[string]shapeRow{}
	inTable := false
	for _, l := range strings.Split(doc[i+len(heading)+2:], "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		cells := strings.Split(strings.Trim(l, "|"), "|")
		if len(cells) < 3 || !strings.Contains(cells[0], "`") {
			continue // header or separator row
		}
		for _, m := range backticked.FindAllStringSubmatch(cells[0], -1) {
			rows[documentedCommand(m[1])] = shapeRow{
				kind:  strings.TrimSpace(cells[1]),
				extra: strings.TrimSpace(cells[2]),
			}
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no table rows after %q", heading)
	}
	return rows
}

// checkCommandSet fails for each declared command a table leaves out and
// each documented command that declares no shape.
func checkCommandSet(t *testing.T, doc string, rows map[string]shapeRow, declared map[string]declaredShape) {
	t.Helper()
	for path, d := range declared {
		row, ok := rows[path]
		if !ok {
			t.Errorf("%s: 'git hop %s' declares a result shape but is not documented", doc, path)
			continue
		}
		if row.kind != d.kind() {
			t.Errorf("%s: 'git hop %s' documented as %q, declares %q", doc, path, row.kind, d.kind())
		}
	}
	for path := range rows {
		if _, ok := declared[path]; !ok {
			t.Errorf("%s: documents 'git hop %s', which declares no result shape", doc, path)
		}
	}
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// headingAnchor is the anchor a markdown heading gets.
func headingAnchor(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// sections splits doc into its ### sections, by anchor.
func sections(doc string) map[string]string {
	out := map[string]string{}
	parts := strings.Split(doc, "\n### ")
	for _, p := range parts[1:] {
		heading, body, _ := strings.Cut(p, "\n")
		if end := strings.Index(body, "\n## "); end >= 0 {
			body = body[:end]
		}
		out[headingAnchor(heading)] = body
	}
	return out
}

// fieldTable is the field names of the first "| Field |" table in body.
func fieldTable(body string) []string {
	var names []string
	inTable := false
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "| Field |") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if !strings.HasPrefix(l, "|") {
			break
		}
		if m := backticked.FindStringSubmatch(strings.Split(strings.Trim(l, "|"), "|")[0]); m != nil {
			names = append(names, m[1])
		}
	}
	return names
}

// jsonExamples are the ```json blocks of body.
func jsonExamples(body string) [][]byte {
	var out [][]byte
	for _, block := range strings.Split(body, "```json\n")[1:] {
		code, _, _ := strings.Cut(block, "```")
		out = append(out, []byte(code))
	}
	return out
}

// Each shape the reference documents matches the declared one: the
// command list, the result kind, the field table, the columns, every
// enum value, and each example, which must decode as the shape with its
// required fields. The schema version it quotes is the current one.
func TestReferenceDocumentsEveryResultShape(t *testing.T) {
	declared := declaredShapes(t)
	doc := readDoc(t, "09-reference.mdx")
	rows := parseShapeTable(t, doc, "### Result shapes")
	checkCommandSet(t, "09-reference.mdx", rows, declared)

	if want := "(currently " + resultSchemaVersion + ")"; !strings.Contains(doc, want) {
		t.Errorf("09-reference.mdx does not quote the result schema version as %q", want)
	}

	// The commands each section documents.
	bySection := map[string][]declaredShape{}
	for path, row := range rows {
		m := mdLink.FindStringSubmatch(row.extra)
		d, ok := declared[path]
		if m == nil || !ok {
			if ok {
				t.Errorf("'git hop %s': shape cell %q links no section", path, row.extra)
			}
			continue
		}
		bySection[m[1]] = append(bySection[m[1]], d)
	}
	secs := sections(doc)
	for anchor, shapes := range bySection {
		body, ok := secs[anchor]
		if !ok {
			t.Errorf("section #%s not found", anchor)
			continue
		}
		sort.Slice(shapes, func(i, j int) bool { return shapes[i].path < shapes[j].path })
		t.Run(anchor, func(t *testing.T) {
			checkSection(t, body, shapes)
		})
	}
}

func checkSection(t *testing.T, body string, shapes []declaredShape) {
	want := map[string]bool{}
	for _, d := range shapes {
		all, _ := d.fields()
		for _, f := range all {
			want[f] = true
		}
	}
	got := map[string]bool{}
	for _, f := range fieldTable(body) {
		got[f] = true
	}
	for f := range want {
		if !got[f] {
			t.Errorf("field %q is not documented", f)
		}
	}
	for f := range got {
		if !want[f] {
			t.Errorf("documented field %q is not in the shape", f)
		}
	}

	var colLine string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "Columns:") {
			colLine = l
		}
	}
	var groups []string
	for _, m := range backticked.FindAllStringSubmatch(colLine, -1) {
		groups = append(groups, m[1])
	}
	for _, d := range shapes {
		cols := d.columns()
		found := false
		for _, g := range groups {
			found = found || g == cols
		}
		if !found {
			t.Errorf("'git hop %s': columns %q are not documented (Columns: line %q)", d.path, cols, colLine)
		}
	}
	for _, g := range groups {
		matched := false
		for _, d := range shapes {
			matched = matched || g == d.columns()
		}
		if !matched {
			t.Errorf("documented columns %q match no shape", g)
		}
	}

	for _, d := range shapes {
		for _, v := range d.enums(t) {
			if !strings.Contains(body, "`"+v+"`") {
				t.Errorf("'git hop %s': enum value %q is not documented", d.path, v)
			}
		}
	}

	examples := jsonExamples(body)
	covered := map[string]bool{}
	for i, ex := range examples {
		ok := false
		for _, d := range shapes {
			if d.decodes(ex) {
				ok = true
				covered[d.path] = true
			}
		}
		if !ok {
			t.Errorf("example %d does not decode as the shape (unknown or missing field):\n%s", i+1, ex)
		}
	}
	for _, d := range shapes {
		if !covered[d.path] {
			t.Errorf("'git hop %s' has no JSON example", d.path)
		}
	}
}

// The agent cheatsheet's shape table lists every command with a result,
// its kind and its columns.
func TestCheatsheetListsEveryResultShape(t *testing.T) {
	declared := declaredShapes(t)
	doc := readDoc(t, "cheatsheet-agent.md")
	rows := parseShapeTable(t, doc, "## Result Shapes")
	checkCommandSet(t, "cheatsheet-agent.md", rows, declared)
	for path, row := range rows {
		d, ok := declared[path]
		if !ok {
			continue
		}
		m := backticked.FindStringSubmatch(row.extra)
		if m == nil || m[1] != d.columns() {
			t.Errorf("cheatsheet-agent.md: 'git hop %s' columns %q, want `%s`", path, row.extra, d.columns())
		}
	}
	if want := "schema " + resultSchemaVersion; !strings.Contains(doc, want) {
		t.Errorf("cheatsheet-agent.md does not quote the result schema version as %q", want)
	}
}

// commandTree must see the root, which declares the switch/clone shape.
func TestDeclaredShapesIncludeTheRoot(t *testing.T) {
	if _, ok := declaredShapes(t)[""]; !ok {
		t.Fatal("the root command's shape is missing")
	}
}
