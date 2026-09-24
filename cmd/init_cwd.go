package cmd

import "os"

// reanchorInitCwd moves the process into repoPath once a conversion has
// run. A bare conversion builds the new hub beside the repository and
// swaps it into place, deleting the directory init was started in; the
// process cwd still names that deleted directory. Every later step, and
// every hook it spawns, would inherit it (bash hooks then fail getcwd).
// repoPath is the same path, now naming the hub, so this puts the process
// back where the user ran init, the way clone's hooks start where clone
// was run. For a regular conversion it is a no-op in effect.
func reanchorInitCwd(repoPath string) error {
	return os.Chdir(repoPath)
}
