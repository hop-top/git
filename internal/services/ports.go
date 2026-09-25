package services

import (
	"fmt"
	"hash/crc32"

	"hop.top/git/internal/config"
)

// PortAllocator handles port allocation
type PortAllocator struct {
	Config *config.PortsConfig
	// Keep holds ports, by service, the worktree already has and keeps.
	Keep map[string]int
	// Reserved holds ports allocated elsewhere, in this hopspace or any
	// other a known hub uses; new ports never land on them.
	Reserved map[int]bool
	// Seed is the hash-mode input; AllocatePorts' branch when empty.
	Seed string
}

// NewPortAllocator creates a new allocator
func NewPortAllocator(cfg *config.PortsConfig) *PortAllocator {
	return &PortAllocator{Config: cfg}
}

// AllocatePorts returns a port for every service of the hopspace: the
// one Keep gives it, else a new one. The services without a port get a
// block of consecutive ports none of Reserved (or Keep) holds: after the
// highest allocated port in incremental mode, at a position hashed from
// Seed in hash mode, and the first free block in range when that one is
// not free.
func (a *PortAllocator) AllocatePorts(branch string) (map[string]int, error) {
	used := make(map[int]bool, len(a.Reserved)+len(a.Keep))
	for p := range a.Reserved {
		used[p] = true
	}
	ports := make(map[string]int)
	var missing []string
	for _, svc := range a.Config.Services {
		if p, ok := a.Keep[svc]; ok {
			ports[svc] = p
			used[p] = true
			continue
		}
		missing = append(missing, svc)
	}
	if len(missing) == 0 {
		if len(ports) == 0 && a.Config.AllocationMode != "incremental" {
			return nil, nil
		}
		return ports, nil
	}

	seed := a.Seed
	if seed == "" {
		seed = branch
	}
	start, err := a.findBlock(len(missing), used, seed)
	if err != nil {
		return nil, err
	}
	for i, svc := range missing {
		ports[svc] = start + i
	}
	return ports, nil
}

// findBlock returns the first port of n consecutive free ports in range.
func (a *PortAllocator) findBlock(n int, used map[int]bool, seed string) (int, error) {
	lo, hi := a.Config.BaseRange.Start, a.Config.BaseRange.End
	if hi-lo <= 0 {
		return 0, fmt.Errorf("invalid port range")
	}
	free := func(start int) bool {
		if start < lo || start+n-1 > hi {
			return false
		}
		for p := start; p < start+n; p++ {
			if used[p] {
				return false
			}
		}
		return true
	}

	if a.Config.AllocationMode == "incremental" {
		next := lo
		for p := range used {
			if p >= lo && p <= hi && p+1 > next {
				next = p + 1
			}
		}
		if free(next) {
			return next, nil
		}
	} else {
		// Positions a block can start at; as earlier releases hashed.
		positions := hi - lo - n + 1
		if positions <= 0 {
			return 0, fmt.Errorf("port range too small for services")
		}
		first := int(crc32.ChecksumIEEE([]byte(seed))) % positions
		for i := 0; i < positions; i++ {
			if start := lo + (first+i)%positions; free(start) {
				return start, nil
			}
		}
	}

	for start := lo; start+n-1 <= hi; start++ {
		if free(start) {
			return start, nil
		}
	}
	return 0, fmt.Errorf("port range exhausted")
}
