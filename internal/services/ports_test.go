package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
)

func allocator(mode string, lo, hi int, services ...string) *PortAllocator {
	return NewPortAllocator(&config.PortsConfig{
		AllocationMode: mode,
		BaseRange:      config.PortRange{Start: lo, End: hi},
		Services:       services,
	})
}

// Kept ports are returned as they are; only services without one get new
// ports, off every reserved and kept port.
func TestAllocatePorts_KeepsAndFillsMissing(t *testing.T) {
	a := allocator("incremental", 10000, 10010, "WEB", "DB")
	a.Keep = map[string]int{"WEB": 10005}
	a.Reserved = map[int]bool{10006: true}

	ports, err := a.AllocatePorts("main")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"WEB": 10005, "DB": 10007}, ports)
}

// Incremental mode continues after the highest port in use anywhere, not
// after the hopspace's own.
func TestAllocatePorts_IncrementalAfterReserved(t *testing.T) {
	a := allocator("incremental", 10000, 20000, "WEB")
	a.Reserved = map[int]bool{10000: true, 10003: true}

	ports, err := a.AllocatePorts("main")
	require.NoError(t, err)
	assert.Equal(t, 10004, ports["WEB"])
}

// Past the end of the range, the first free block is used.
func TestAllocatePorts_IncrementalWrapsToFreeBlock(t *testing.T) {
	a := allocator("incremental", 10000, 10003, "WEB", "DB")
	a.Reserved = map[int]bool{10000: true, 10003: true}

	ports, err := a.AllocatePorts("main")
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"WEB": 10001, "DB": 10002}, ports)

	a.Reserved = map[int]bool{10000: true, 10002: true}
	_, err = a.AllocatePorts("main")
	require.Error(t, err, "no two consecutive free ports")
}

// Hash mode starts where the seed hashes to and probes past reserved
// ports.
func TestAllocatePorts_HashProbesPastReserved(t *testing.T) {
	a := allocator("hash", 10000, 10100, "WEB")
	a.Seed = "org/repo/app-1a2b3c4d/main"
	first, err := a.AllocatePorts("main")
	require.NoError(t, err)

	a.Reserved = map[int]bool{first["WEB"]: true}
	second, err := a.AllocatePorts("main")
	require.NoError(t, err)
	assert.NotEqual(t, first["WEB"], second["WEB"])

	a.Seed = "org/repo/other-5e6f7a8b/main"
	a.Reserved = nil
	other, err := a.AllocatePorts("main")
	require.NoError(t, err)
	assert.NotEqual(t, first["WEB"], other["WEB"], "the hub is part of the hash input")
}
