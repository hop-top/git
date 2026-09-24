package script

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/rogpeppe/go-internal/testscript"
	"gopkg.in/yaml.v3"
)

// cmdDecode implements the `decode` script command:
//
//	decode json|yaml <file> <path> [want]
//
// It parses <file> (stdout/stderr name the last command's streams) as exactly
// one JSON or YAML document -- trailing data is a failure, so human text
// leaking onto a structured stream cannot hide behind a valid prefix -- and
// then resolves <path> inside it.
//
// <path> is dot-separated: object keys, integer array indices, and a final
// `#` for the length of the array or object reached so far. `.` is the
// document root. With [want], the resolved value (formatted with %v) must
// equal it, or match it as a regexp when [want] starts with `~`. Without
// [want], the path must merely resolve. Negated (`! decode ...`), the path
// must NOT resolve; the document must still parse.
func cmdDecode(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 3 || len(args) > 4 {
		ts.Fatalf("usage: decode json|yaml <file> <path> [want]")
	}
	doc, err := decodeDocument(args[0], ts.ReadFile(args[1]))
	if err != nil {
		ts.Fatalf("%s is not a single %s document: %v", args[1], args[0], err)
	}

	got, err := resolvePath(doc, args[2])
	if neg {
		if err == nil && len(args) == 3 {
			ts.Fatalf("path %q unexpectedly resolved to %v", args[2], got)
		}
		if err == nil && len(args) == 4 && matchWant(ts, got, args[3]) {
			ts.Fatalf("path %q unexpectedly matched %q", args[2], args[3])
		}
		return
	}
	if err != nil {
		ts.Fatalf("path %q: %v", args[2], err)
	}
	if len(args) == 4 && !matchWant(ts, got, args[3]) {
		ts.Fatalf("path %q = %v, want %q", args[2], got, args[3])
	}
}

func decodeDocument(format, raw string) (any, error) {
	var doc any
	switch format {
	case "json":
		dec := json.NewDecoder(strings.NewReader(raw))
		if err := dec.Decode(&doc); err != nil {
			return nil, err
		}
		var extra any
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("trailing data after the document")
		}
	case "yaml":
		dec := yaml.NewDecoder(strings.NewReader(raw))
		if err := dec.Decode(&doc); err != nil {
			return nil, err
		}
		var extra any
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("trailing data after the document")
		}
	default:
		return nil, fmt.Errorf("unknown format %q (want json or yaml)", format)
	}
	return doc, nil
}

func resolvePath(doc any, path string) (any, error) {
	if path == "." || path == "" {
		return doc, nil
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			if seg == "#" {
				cur = len(node)
				continue
			}
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("no key %q", seg)
			}
			cur = v
		case []any:
			if seg == "#" {
				cur = len(node)
				continue
			}
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("index %q out of range (len %d)", seg, len(node))
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("cannot descend into %T with %q", cur, seg)
		}
	}
	return cur, nil
}

func matchWant(ts *testscript.TestScript, got any, want string) bool {
	s := fmt.Sprintf("%v", got)
	if got == nil {
		s = "null"
	}
	if strings.HasPrefix(want, "~") {
		re, err := regexp.Compile(want[1:])
		if err != nil {
			ts.Fatalf("bad regexp %q: %v", want[1:], err)
		}
		return re.MatchString(s)
	}
	return s == want
}
