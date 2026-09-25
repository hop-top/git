package hop

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/afero"
)

// settings.envPatterns and migrated were written into every hop.json but
// never read. New hubs no longer get them.
func TestNewHopJSON_HasNoUnreadMembers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(fs afero.Fs, root string) error
	}{
		{"create hub", func(fs afero.Fs, root string) error {
			_, err := CreateHub(fs, root, "https://github.com/org/repo.git", "org", "repo", "main")
			return err
		}},
		{"clone local", func(fs afero.Fs, root string) error {
			return createMergedConfig(fs, root, "https://github.com/org/repo.git", "org", "repo", "main", root+"/hops/main")
		}},
		{"clone global", func(fs afero.Fs, root string) error {
			return createProjectConfig(fs, root, "https://github.com/org/repo.git", "org", "repo", "main")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			if err := tc.write(fs, "/hub"); err != nil {
				t.Fatal(err)
			}
			doc := readHopJSONDoc(t, fs, "/hub")
			if _, ok := doc["migrated"]; ok {
				t.Errorf("hop.json has migrated: %v", doc)
			}
			if settings, ok := doc["settings"].(map[string]any); ok {
				if _, ok := settings["envPatterns"]; ok {
					t.Errorf("hop.json has settings.envPatterns: %v", doc)
				}
			}
		})
	}
}

// A hop.json an earlier release wrote keeps both members through every
// rewrite, hub side and hopspace side alike.
func TestRewrite_KeepsUnreadMembersOfEarlierRelease(t *testing.T) {
	fs := afero.NewMemMapFs()
	old := `{
  "repo": {"uri": "https://github.com/org/repo.git", "org": "org", "repo": "repo", "defaultBranch": "main"},
  "branches": {"main": {"path": "/hub/hops/main", "hopspaceBranch": "main"}},
  "settings": {"envPatterns": ["dev", "staging", "qa"], "compareBranch": "develop"},
  "migrated": true,
  "forks": null
}`
	if err := afero.WriteFile(fs, "/hub/hop.json", []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	hub, err := LoadHub(fs, "/hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.AddBranch("feat", "feat", "/hub/hops/feat"); err != nil {
		t.Fatal(err)
	}
	space, err := LoadHopspace(fs, "/hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := space.RegisterBranch("feat", "/hub/hops/feat"); err != nil {
		t.Fatal(err)
	}
	if err := hub.RemoveBranch("main"); err != nil {
		t.Fatal(err)
	}

	doc := readHopJSONDoc(t, fs, "/hub")
	if doc["migrated"] != true {
		t.Errorf("migrated = %v, want true", doc["migrated"])
	}
	settings, _ := doc["settings"].(map[string]any)
	want := []any{"dev", "staging", "qa"}
	if !reflect.DeepEqual(settings["envPatterns"], want) {
		t.Errorf("settings.envPatterns = %v, want %v", settings["envPatterns"], want)
	}
	if settings["compareBranch"] != "develop" {
		t.Errorf("settings.compareBranch = %v, want develop", settings["compareBranch"])
	}
}

func readHopJSONDoc(t *testing.T, fs afero.Fs, dir string) map[string]any {
	t.Helper()
	data, err := afero.ReadFile(fs, filepath.Join(dir, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
