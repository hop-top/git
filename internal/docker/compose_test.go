package docker

import (
	"testing"

	"github.com/spf13/afero"
)

func TestFindComposeFile_UsesGivenFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/wt/docker-compose.yml", []byte("services: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := FindComposeFile(fs, "/wt"); got != "docker-compose.yml" {
		t.Fatalf("FindComposeFile = %q, want docker-compose.yml (file exists only on the given fs)", got)
	}
}

func TestFindComposeFile_PriorityOrder(t *testing.T) {
	fs := afero.NewMemMapFs()
	for _, name := range []string{"docker-compose.yaml", "compose.yml"} {
		if err := afero.WriteFile(fs, "/wt/"+name, []byte("services: {}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if got := FindComposeFile(fs, "/wt"); got != "compose.yml" {
		t.Fatalf("FindComposeFile = %q, want compose.yml", got)
	}
	if got := FindComposeFile(fs, "/empty"); got != "" {
		t.Fatalf("FindComposeFile on dir without compose file = %q, want empty", got)
	}
}
