package xrrx_test

import (
	"strings"
	"testing"

	"hop.top/git/internal/xrrx"
)

func TestSessionFromEnv_Unset_ReturnsNil(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "")
	t.Setenv(xrrx.EnvCassetteDir, "")

	sess, err := xrrx.SessionFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil session, got %v", sess)
	}
}

func TestSessionFromEnv_Off_ReturnsNil(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "off")
	t.Setenv(xrrx.EnvCassetteDir, "/tmp/anything")

	sess, err := xrrx.SessionFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess != nil {
		t.Fatalf("expected nil session for off mode, got %v", sess)
	}
}

func TestSessionFromEnv_Record_ReturnsSession(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "record")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	sess, err := xrrx.SessionFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session for record mode")
	}
}

func TestSessionFromEnv_Replay_ReturnsSession(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "replay")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	sess, err := xrrx.SessionFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session for replay mode")
	}
}

func TestSessionFromEnv_Passthrough_ReturnsSession(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "passthrough")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	sess, err := xrrx.SessionFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session for passthrough mode")
	}
}

func TestSessionFromEnv_ModeSetButDirMissing_ReturnsError(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "record")
	t.Setenv(xrrx.EnvCassetteDir, "")

	_, err := xrrx.SessionFromEnv()
	if err == nil {
		t.Fatal("expected error when XRR_MODE set but XRR_CASSETTE_DIR unset")
	}
	if !strings.Contains(err.Error(), "XRR_CASSETTE_DIR") {
		t.Fatalf("error %q should mention XRR_CASSETTE_DIR", err)
	}
}

func TestSessionFromEnv_InvalidMode_ReturnsError(t *testing.T) {
	t.Setenv(xrrx.EnvMode, "yolo")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	_, err := xrrx.SessionFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid XRR_MODE")
	}
	if !strings.Contains(err.Error(), "yolo") {
		t.Fatalf("error %q should echo invalid value", err)
	}
}
