package services

import (
	"strings"
	"testing"

	"hop.top/git/internal/config"
	"github.com/spf13/afero"
)

// TestWriteEnvFile_PortKeyNaming verifies that HOP_PORT_* keys in .env
// are written with single prefix, not doubled (HOP_PORT_HOP_PORT_WEB).
// Regression test for: portVarNames from docker override contain full var names
// like "HOP_PORT_WEB"; these must be stripped before writeEnvFile adds prefix.
func TestWriteEnvFile_PortKeyNaming(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.PortsConfig{
		BaseRange: config.PortRange{Start: 10000, End: 20000},
		Services:  []string{},
	}
	m := NewEnvManager(fs, cfg, &config.VolumesConfig{}, nil)

	ports := map[string]int{
		"WEB":   12345,
		"CACHE": 12346,
	}
	vols := map[string]string{}

	if err := m.writeEnvFile("/test/.env", ports, vols); err != nil {
		t.Fatalf("writeEnvFile error: %v", err)
	}

	data, err := afero.ReadFile(fs, "/test/.env")
	if err != nil {
		t.Fatalf("read .env error: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "HOP_PORT_WEB=12345") {
		t.Errorf("expected HOP_PORT_WEB=12345 in .env, got:\n%s", content)
	}
	if !strings.Contains(content, "HOP_PORT_CACHE=12346") {
		t.Errorf("expected HOP_PORT_CACHE=12346 in .env, got:\n%s", content)
	}

	// Confirm no double-prefix
	if strings.Contains(content, "HOP_PORT_HOP_PORT_") {
		t.Errorf("double-prefix detected in .env:\n%s", content)
	}
}

// TestEnvManager_PortVarNamesSuffix verifies that when portVarNames
// (full names like "HOP_PORT_WEB") are appended to Config.Services,
// the suffix is stored (not the full name), so writeEnvFile doesn't double-prefix.
func TestEnvManager_PortVarNamesSuffix(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.PortsConfig{
		BaseRange: config.PortRange{Start: 10000, End: 20000},
		Services:  []string{},
	}
	m := NewEnvManager(fs, cfg, &config.VolumesConfig{}, nil)

	// Simulate what env.go does: strip prefix, add to Config.Services
	portVarNames := []string{"HOP_PORT_WEB", "HOP_PORT_CACHE"}
	existingSvcs := make(map[string]bool)
	for _, s := range m.Ports.Config.Services {
		existingSvcs[s] = true
	}
	for _, name := range portVarNames {
		svcKey := strings.TrimPrefix(name, "HOP_PORT_")
		if !existingSvcs[svcKey] {
			m.Ports.Config.Services = append(m.Ports.Config.Services, svcKey)
		}
	}

	// Allocate ports for a branch
	ports, err := m.Ports.AllocatePorts("feature-branch")
	if err != nil {
		t.Fatalf("AllocatePorts error: %v", err)
	}

	// Keys should be suffixes (WEB, CACHE), not full names
	for key := range ports {
		if strings.HasPrefix(key, "HOP_PORT_") {
			t.Errorf("port map key has HOP_PORT_ prefix (double-prefix would occur): %s", key)
		}
	}

	if _, ok := ports["WEB"]; !ok {
		t.Errorf("expected key WEB in ports map, got: %v", ports)
	}
	if _, ok := ports["CACHE"]; !ok {
		t.Errorf("expected key CACHE in ports map, got: %v", ports)
	}

	// writeEnvFile should produce correct keys
	if err := m.writeEnvFile("/out/.env", ports, map[string]string{}); err != nil {
		t.Fatalf("writeEnvFile error: %v", err)
	}
	data, _ := afero.ReadFile(fs, "/out/.env")
	content := string(data)

	if strings.Contains(content, "HOP_PORT_HOP_PORT_") {
		t.Errorf("double-prefix in .env:\n%s", content)
	}
	if !strings.Contains(content, "HOP_PORT_WEB=") {
		t.Errorf("HOP_PORT_WEB missing from .env:\n%s", content)
	}
	if !strings.Contains(content, "HOP_PORT_CACHE=") {
		t.Errorf("HOP_PORT_CACHE missing from .env:\n%s", content)
	}
}
