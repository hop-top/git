package cmd

import "testing"

func TestDescribeStartPoint(t *testing.T) {
	tests := []struct {
		startPoint, defaultBranch, want string
	}{
		{"", "main", "default branch 'main'"},
		{"default-branch", "main", "default branch 'main'"},
		{"", "", "HEAD"},
		{"initial", "main", "the root commit ('initial')"},
		{"v2.3.0", "main", "'v2.3.0'"},
	}
	for _, tt := range tests {
		if got := describeStartPoint(tt.startPoint, tt.defaultBranch); got != tt.want {
			t.Errorf("describeStartPoint(%q, %q) = %q, want %q", tt.startPoint, tt.defaultBranch, got, tt.want)
		}
	}
}

func TestDisplayPath(t *testing.T) {
	tests := []struct {
		cwd, path, want string
	}{
		{"/hub", "/hub/hops/feat/x", "./hops/feat/x"},
		{"/hub/hops/main", "/hub/hops/feat/x", "../feat/x"},
	}
	for _, tt := range tests {
		if got := displayPath(tt.cwd, tt.path); got != tt.want {
			t.Errorf("displayPath(%q, %q) = %q, want %q", tt.cwd, tt.path, got, tt.want)
		}
	}
}
