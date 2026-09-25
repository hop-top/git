package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"hop.top/git/internal/config"
)

// modeledKeys collects every JSON member name t can hold, recursing into
// struct fields, pointers, slices and map values.
func modeledKeys(t reflect.Type, into map[string]bool) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		modeledKeys(t.Elem(), into)
	case reflect.Struct:
		if t.PkgPath() == "time" {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			into[name] = true
			modeledKeys(f.Type, into)
		}
	}
}

// The reference's hop.json schema documents only members git-hop models:
// a documented key the code never reads is a setting that silently does
// nothing.
func TestReferenceHopJSONSchema_DocumentsOnlyModeledKeys(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "09-reference.mdx"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start := strings.Index(doc, "### hop.json Schema")
	if start < 0 {
		t.Fatal("09-reference.mdx has no hop.json Schema section")
	}
	section := doc[start:]
	block := regexp.MustCompile("(?s)```json\n(.*?)```").FindStringSubmatch(section)
	if block == nil {
		t.Fatal("hop.json Schema section has no json block")
	}

	modeled := map[string]bool{}
	modeledKeys(reflect.TypeOf(config.HubConfig{}), modeled)
	modeledKeys(reflect.TypeOf(config.HopspaceConfig{}), modeled)

	keys := regexp.MustCompile(`"([A-Za-z_]+)"\s*:`).FindAllStringSubmatch(block[1], -1)
	if len(keys) == 0 {
		t.Fatal("no keys found in the hop.json schema block")
	}
	for _, k := range keys {
		if !modeled[k[1]] {
			t.Errorf("docs/09-reference.mdx hop.json schema documents %q, which no hop.json type models", k[1])
		}
	}
}
