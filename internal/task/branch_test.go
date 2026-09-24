package task

import (
	"strings"
	"testing"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		name, title, want string
	}{
		{"lowercases and hyphenates", "Login Redirect Loops", "login-redirect-loops"},
		{"collapses punctuation runs", "git hop add --task <id>: derive branch", "git-hop-add-task-id-derive-branch"},
		{"trims leading and trailing separators", "  --Hello, world!--  ", "hello-world"},
		{"keeps digits", "Support HTTP/2 on port 8080", "support-http-2-on-port-8080"},
		{"drops accents", "Café naïve résumé", "cafe-naive-resume"},
		{"drops non-latin text", "修复 login 问题", "login"},
		{"nothing usable", "!!! ??? ***", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Slug(tt.title); got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestSlug_BoundedLength(t *testing.T) {
	title := "record the task id in hop json and expose it to every lifecycle hook that runs for the branch"
	got := Slug(title)
	if len(got) > MaxSlugLen {
		t.Fatalf("len(Slug) = %d, want <= %d (%q)", len(got), MaxSlugLen, got)
	}
	if strings.HasSuffix(got, "-") || strings.HasPrefix(got, "-") {
		t.Errorf("Slug = %q, has a dangling hyphen", got)
	}
	// Cut on a word boundary: every word kept is whole.
	for _, w := range strings.Split(got, "-") {
		if !strings.Contains(" "+title+" ", " "+w+" ") {
			t.Errorf("Slug = %q, word %q was cut mid-word", got, w)
		}
	}
}

func TestSlug_LongSingleWordIsTruncated(t *testing.T) {
	got := Slug(strings.Repeat("a", 3*MaxSlugLen))
	if got != strings.Repeat("a", MaxSlugLen) {
		t.Errorf("Slug = %q, want %d a's", got, MaxSlugLen)
	}
}

func TestCommitType(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{"no tags defaults to feat", nil, "feat"},
		{"no type tag defaults to feat", []string{"area:auth", "cmd:add"}, "feat"},
		{"conventional type kept", []string{"type:fix"}, "fix"},
		{"every conventional type", []string{"type:perf"}, "perf"},
		{"case-insensitive", []string{"TYPE:Docs"}, "docs"},
		{"bug maps to fix", []string{"type:bug"}, "fix"},
		{"bugfix maps to fix", []string{"type:bugfix"}, "fix"},
		{"feature maps to feat", []string{"type:feature"}, "feat"},
		{"doc maps to docs", []string{"type:doc"}, "docs"},
		{"tests maps to test", []string{"type:tests"}, "test"},
		{"unknown type defaults to feat", []string{"type:spike"}, "feat"},
		{"first recognized type wins", []string{"type:spike", "type:chore", "type:fix"}, "chore"},
		{"type tag among others", []string{"area:auth", "type:refactor"}, "refactor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CommitType(tt.tags); got != tt.want {
				t.Errorf("CommitType(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestBranchName(t *testing.T) {
	got, err := BranchName(Task{ID: "T-9", Title: "Login redirect loops", Tags: []string{"type:bug"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "fix/login-redirect-loops" {
		t.Errorf("BranchName = %q, want fix/login-redirect-loops", got)
	}
}

func TestBranchName_NeverContainsID(t *testing.T) {
	got, err := BranchName(Task{ID: "T-9", Title: "Handle T-9 edge case"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "feat/handle-t-9-edge-case" {
		t.Errorf("BranchName = %q", got)
	}
	got, err = BranchName(Task{ID: "T-9", Title: "Plain title"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(got), "t-9") {
		t.Errorf("BranchName = %q, contains the task id", got)
	}
}

func TestBranchName_UnusableTitle(t *testing.T) {
	if _, err := BranchName(Task{ID: "T-9", Title: "???"}); err == nil {
		t.Fatal("BranchName with no usable title succeeded, want error")
	}
}

func TestValidateID(t *testing.T) {
	for _, ok := range []string{"T-1", "42", "proj/T-0042", "ABC-123"} {
		if err := ValidateID(ok); err != nil {
			t.Errorf("ValidateID(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", "-rf", "--format", "T 1", "T-1\n", "a\x00b"} {
		if err := ValidateID(bad); err == nil {
			t.Errorf("ValidateID(%q) = nil, want error", bad)
		}
	}
}
