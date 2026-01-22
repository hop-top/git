package services

import (
	"fmt"
	"hash/crc32"

	"github.com/jadb/git-hop/internal/config"
)

// PortAllocator handles port allocation
type PortAllocator struct {
	Config *config.PortsConfig
}

// NewPortAllocator creates a new allocator
func NewPortAllocator(cfg *config.PortsConfig) *PortAllocator {
	return &PortAllocator{Config: cfg}
}

// AllocatePorts allocates ports for a branch
func (a *PortAllocator) AllocatePorts(branch string) (map[string]int, error) {
	if a.Config.AllocationMode == "incremental" {
		return a.allocateIncremental(branch)
	}
	return a.allocateHash(branch)
}

func (a *PortAllocator) allocateIncremental(branch string) (map[string]int, error) {
	// Find the highest used port
	maxPort := a.Config.BaseRange.Start - 1
	for _, b := range a.Config.Branches {
		for _, p := range b.Ports {
			if p > maxPort {
				maxPort = p
			}
		}
	}

	startPort := maxPort + 1
	ports := make(map[string]int)
	for i, svc := range a.Config.Services {
		port := startPort + i
		if port > a.Config.BaseRange.End {
			return nil, fmt.Errorf("port range exhausted")
		}
		ports[svc] = port
	}
	return ports, nil
}

func (a *PortAllocator) allocateHash(branch string) (map[string]int, error) {
	// Use CRC32 to pick a starting port
	hash := crc32.ChecksumIEEE([]byte(branch))
	rangeSize := a.Config.BaseRange.End - a.Config.BaseRange.Start
	if rangeSize <= 0 {
		return nil, fmt.Errorf("invalid port range")
	}

	// We need a block of ports equal to len(Services)
	blockSize := len(a.Config.Services)
	if blockSize == 0 {
		return nil, nil
	}

	// Ensure we fit in the range
	// effectiveRange is the number of possible starting blocks
	effectiveRange := rangeSize - blockSize + 1
	if effectiveRange <= 0 {
		return nil, fmt.Errorf("port range too small for services")
	}

	offset := int(hash) % effectiveRange
	startPort := a.Config.BaseRange.Start + offset

	ports := make(map[string]int)
	for i, svc := range a.Config.Services {
		ports[svc] = startPort + i
	}

	// Check for collisions?
	// Hash mode implies potential collisions.
	// We could check and re-hash or shift, but for now let's stick to simple hash.
	// The user chose hash mode knowing this.

	return ports, nil
}
