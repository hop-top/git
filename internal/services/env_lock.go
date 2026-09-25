package services

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/filelock"
	"hop.top/git/internal/state"
)

// ports.json and volumes.json are rewritten by env generate (and add,
// clone and init, through GenerateWorktreeEnv), remove, remove --hub,
// move and prune, and several of those can run at once. Each rewrite is
// a load-modify-save; without serialising them, a run that loaded a file
// before another run saved it writes its stale copy back, undoing the
// other run (a removed entry reappears, an allocation disappears and its
// ports are handed out again).
//
// Two locks serialise them, both filelock.Guards:
//
//   - ports.json.lock, next to ports.json, guards ports.json and
//     volumes.json of that hopspace together. Every writer changes both
//     under the same keys (an allocation records both, remove drops both,
//     move rekeys both), so one lock keeps the pair consistent and needs
//     no order between two. Taken by every load-modify-save of them.
//
//   - port-allocation.lock, in the state home, serialises allocations.
//     An allocation picks ports no hub git-hop knows holds, reading the
//     ports.json of every hub in state, not only its own hopspace's; two
//     allocations in different hopspaces would otherwise both pick the
//     same free ports. Taken only by GenerateWorktreeEnv, around reading
//     every hub's records, allocating and recording the result. Other
//     writers only drop or rekey entries, which frees no port an
//     allocation could pick twice, so they do not take it.
//
// Lock order: port-allocation.lock, then ports.json.lock. Neither is held
// with hop.json.lock or state.json.lock: hop.json and state.json are only
// read under them (whole files, replaced by rename), and no writer of
// either takes these. Both are held only around the load-modify-save:
// never across docker or compose calls, hooks, git commands or anything
// else that may run git-hop, which would wait on the lock this run holds.
//
// Saves go through config.Writer: a temp file named config.IsTempName
// next to the file, renamed over it, the same as hop.json, so prune
// sweeps the ones an interrupted save leaves (hop.SweepHopJSONTemps).

// PortsLockName is the lock file next to ports.json that guards it and
// volumes.json.
const PortsLockName = "ports.json.lock"

// AllocationLockName is the lock file in the state home that serialises
// port allocations across every hopspace.
const AllocationLockName = "port-allocation.lock"

// envLockTimeout bounds the wait for a lock another live process holds.
// The hold is one load-modify-save, so reaching it means that process is
// stuck.
var envLockTimeout = 30 * time.Second

// ErrEnvLocked is returned when ports.json.lock stays held past the
// timeout.
var ErrEnvLocked = errors.New("ports.json and volumes.json are locked by another git-hop process")

// ErrAllocationLocked is returned when port-allocation.lock stays held
// past the timeout.
var ErrAllocationLocked = errors.New("port allocation is locked by another git-hop process")

// withEnvLock runs fn while holding the lock on ports.json and
// volumes.json in hopspacePath. fn loads them afresh and must not take
// the same lock again.
func withEnvLock(fs afero.Fs, hopspacePath string, fn func() error) error {
	return filelock.Guard{
		Path:    filepath.Join(hopspacePath, PortsLockName),
		Timeout: envLockTimeout,
		Busy:    ErrEnvLocked,
	}.Do(fs, fn)
}

// withAllocationLock runs fn while holding the port allocation lock, and
// within it the lock on the ports.json and volumes.json of hopspacePath
// (see the lock order above).
func withAllocationLock(fs afero.Fs, hopspacePath string, fn func() error) error {
	return filelock.Guard{
		Path:    filepath.Join(state.GetStateHome(), AllocationLockName),
		Timeout: envLockTimeout,
		Busy:    ErrAllocationLocked,
	}.Do(fs, func() error {
		return withEnvLock(fs, hopspacePath, fn)
	})
}
