package git

// FetchArgs is the argument list of every fetch git-hop runs: "git"
// followed by FetchArgs(args...) is `git fetch args...` with git's
// auto-maintenance switched off for that one command.
//
// A fetch ends by starting `git maintenance run --auto`, detached. Since
// git 2.54 that run prunes worktree entries that have neither a gitdir
// file nor a lock, which is exactly what `git worktree add` leaves for a
// moment between creating worktrees/<name> and locking it. git-hop adds
// a worktree right after most of its fetches, and a prune landing in
// that moment kills the add ("could not open 'worktrees/<name>/locked'
// for writing"). The user's own maintenance still runs on their next git
// command. The config key, not --no-auto-maintenance, keeps git older
// than 2.29 working.
func FetchArgs(args ...string) []string {
	return append([]string{"-c", "maintenance.auto=false", "fetch"}, args...)
}
