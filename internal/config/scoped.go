package config

// ScopedSetting is a hop.* setting as resolved for one repository.
type ScopedSetting struct {
	// Value is the value in effect.
	Value string
	// Raw is the configured value git config gives precedence, "" when
	// the key is set nowhere.
	Raw string
	// Scope is the git config scope Raw came from ("global", "local",
	// "worktree", "command" for `git -c`, "system"); "" when unset.
	Scope string
	// Err says why Raw is unusable, in which case Value is the --global
	// value, or the default when that is unset or unusable too.
	Err error
}

// ResolveScoped resolves key for the repository at dir the way git
// resolves any setting: a repository (local or worktree) value overrides
// --global, and `git -c` overrides both. With dir empty, or not a
// directory git can enter, there is no repository: only the system,
// --global and `git -c` values count, whatever repository the process
// runs in.
//
// valid reports why a value is unusable (nil when it is usable). An
// unusable value in effect gives way to the --global value, or to the
// key's default when that is unset or unusable too; Err says why.
func ResolveScoped(dir, key string, valid func(string) error) ScopedSetting {
	def := Default(key)
	vals, ok := ScopedValues(dir, key)
	if !ok || len(vals) == 0 {
		return ScopedSetting{Value: def}
	}
	eff := vals[len(vals)-1]
	s := ScopedSetting{Value: eff.Value, Raw: eff.Value, Scope: eff.Scope}
	if s.Err = valid(eff.Value); s.Err == nil {
		return s
	}
	s.Value = def
	if eff.Scope != "global" {
		for i := len(vals) - 1; i >= 0; i-- {
			if vals[i].Scope == "global" {
				if valid(vals[i].Value) == nil {
					s.Value = vals[i].Value
				}
				break
			}
		}
	}
	return s
}

// ScopedValues lists the values of key that apply to the repository at
// dir, lowest precedence first. ok is false when git config cannot be
// read at all.
func ScopedValues(dir, key string) (vals []ScopedValue, ok bool) {
	if dir != "" {
		if vals, err := NewGitConfigIn(dir).GetAllScoped(key); err == nil {
			return vals, true
		}
	}
	all, err := NewGitConfig().GetAllScoped(key)
	if err != nil {
		return nil, false
	}
	for _, v := range all {
		if v.Scope != "local" && v.Scope != "worktree" {
			vals = append(vals, v)
		}
	}
	return vals, true
}
