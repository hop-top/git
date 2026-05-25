# Releases

How `git-hop` ships. The pipeline is owned end-to-end by
[release-please](https://github.com/googleapis/release-please) wired via the
shared `hop-top/.github` workflows. The old hand-rolled
flow (manual `git tag` + hand-edited `CHANGELOG.md` + hand-edited
`.github/version.json`) is gone — see [What you do NOT do
anymore](#what-you-do-not-do-anymore).

## What you do

For a normal release:

1. **Land Conventional Commits on `main` as usual.** Allowed types:
   `feat | fix | refactor | build | ci | chore | docs | style | perf | test`
   plus the custom `hop:` type that lands in the Interhop changelog
   section. Breaking changes use `!` or a `BREAKING CHANGE:` trailer.

2. **Watch the standing release PR.** On each merge to `main`, the
   `release-please` workflow opens (or refreshes) a PR titled
   `chore(release): git-hop X.Y.Z-alpha.N`, authored by
   `release-bot[bot]`. The PR accumulates every Conventional Commit
   since the last release into a proposed `CHANGELOG.md` diff plus a
   bumped `.github/version.json`.

3. **Ship by merging that PR.** When you're ready to cut a release,
   merge the release PR. release-please then:
   - pushes the tag `git-hop/vX.Y.Z-alpha.N`,
   - creates the matching GitHub Release,
   - proxy.golang.org picks the tag up within minutes.

4. **Install the new version.**

   ```sh
   go install hop.top/git@git-hop/v0.1.0-alpha.2   # pin to the exact tag
   go install hop.top/git@latest                    # resolve to highest semver
   ```

That's the whole loop. Every step after committing is automated.

## What you do NOT do anymore

- **Do NOT hand-edit `CHANGELOG.md`.** release-please owns it
  end-to-end. Manual edits are overwritten on the next release-PR
  refresh. The file holds a seed comment exactly so this rule is
  visible in-place.
- **Do NOT hand-edit `.github/version.json`.** release-please bumps
  it through the `extra-files` binding (`path:
  .github/version.json`, `jsonpath: $.version`) every time the
  release PR refreshes.
- **Do NOT run `git tag` or `git push --tags` manually for a
  release.** Merging the release PR is the only sanctioned trigger.
  Pushing a tag by hand bypasses the CHANGELOG/version bump and
  leaves the manifest out of sync with the tag — release-please's
  next run will propose a regression.
- **Do NOT use a `Release-As:` footer just to bump the alpha
  counter.** The four-piece prerelease combo (`prerelease: true`,
  `prerelease-type: "alpha.0"`, `versioning: "prerelease"`,
  `bump-minor-pre-major: true`) handles `alpha.N → alpha.N+1`
  automatically. `Release-As:` is reserved for the escape hatches
  below.

## Escape hatches

Three sanctioned overrides. Each has an explicit gate.

### Hotfix backport

When a bug ships in a tagged release and `main` has drifted past
the safe-to-ship line:

1. Branch from the release tag: `git switch -c hotfix-X.Y.Z
   git-hop/vX.Y.Z-alpha.N`.
2. Cherry-pick the fix commits onto the hotfix branch. Each
   cherry-pick keeps its original Conventional Commit message.
3. Dry-run release-please against the hotfix branch BEFORE merging:

   ```sh
   npx release-please@latest release-pr \
     --token "$(gh auth token)" \
     --repo-url https://github.com/hop-top/git \
     --config-file .github/release-please-config.json \
     --manifest-file .github/.release-please-manifest.json \
     --target-branch hotfix-X.Y.Z \
     --dry-run
   ```

4. If the dry-run proposes the expected counter bump, merge the
   hotfix release PR. Otherwise, diagnose before merging.

Reference: hop-top/.github SKILL `references/quick-start.md`.

### First stable cut

The prerelease suffix is sticky — only an explicit footer escapes
it. To cut `1.0.0`:

1. Land the triggering commit with a `Release-As:` footer:

   ```
   feat: <whatever the stable-cut signal is>

   Release-As: 1.0.0
   ```

2. **Team review gate:** the PR cutting stable requires sign-off
   from at least one reviewer beyond the author. Stable is a
   commitment to SemVer guarantees — verify before merging that
   the public API surface is stable, the changelog reads cleanly,
   and downstream consumers have been notified.
3. Merge the release PR as usual.

Reference: hop-top/.github SKILL
`references/how-to/prerelease-channel.md` § "Leave the channel
(cut stable)".

### Emergency retag

Out of scope for this runbook. If a tag was published with a
broken artifact and needs to be re-cut, follow the dedicated SKILL
runbook:
`~/.w/ideacrafterslabs/dotgithub/references/how-to/retrigger-failed-publish.md`.
Do NOT delete + force-push tags by hand — proxy.golang.org caches
tag content and re-pushing the same tag with different content
produces ambiguous module versions in the wild.

## Why there is no publish.yml

The hop-top/.github decision table for single-language repos is
explicit: for Go-only repos, **drop `publish.yml` entirely**. This
repo follows that path.

### Why it's intentionally absent

`publish-on-tag.yml` has two jobs to do — publish to a language
registry, and push a read-only mirror. Neither applies here:

- **No language-registry publish.** Go has no
  `publish-from-source` step. proxy.golang.org pulls tags directly
  from this repo within minutes of a tag push.
- **No mirror to push.** Go always takes the bare-name slot in the
  hop-top org (`hop-top/git` is the canonical repo for
  `hop.top/git`). There is no second-slot `hop-top/git-go` mirror
  — convention forbids it, and the vanity resolver depends on the
  bare slot pointing at the Go source of truth. With nothing to
  mirror to, the mirror job would be a no-op at best and an
  upstream tag-shape mismatch at worst.

Keeping `publish.yml` anyway would only fire the unwanted mirror
push, with no offsetting benefit.

### How a release actually ships

The full chain, no `publish.yml` involved:

```
merge release PR
    ↓
release-please-action pushes git-hop/vX.Y.Z-alpha.N
    ↓
GitHub Release is created from the tag
    ↓
proxy.golang.org indexer picks the tag up (≤ a few minutes)
    ↓
go install hop.top/git@git-hop/vX.Y.Z-alpha.N  works
```

That's the whole thing. The release-please workflow and the Go
module proxy together cover what `publish.yml` would do in a
polyglot repo.

### When to revisit

Add `publish.yml` only if `hop.top/git` ships a non-Go artifact.
Triggers worth noting:

- A TypeScript wrapper (would need an `npm publish` step).
- A Homebrew tap formula for `git-hop` binaries (Scoop/WinGet
  parallel).
- Signed release binaries (would need
  GoReleaser/ship-binaries machinery).

For any of those, follow
`~/.w/ideacrafterslabs/dotgithub/references/how-to/ship-binaries.md`
to add `publish.yml` at that point. Until then, leaving it out is
the documented choice.

## Vanity URL

End users install via the vanity path `hop.top/git`, never via
the bare GitHub path. The mapping:

```
hop.top/git   →   github.com/hop-top/git
```

### Resolver mechanism

A Cloudflare Worker bound to `hop.top/*` answers `?go-get=1` by
checking `hop-top/homebrew-tap` for a `<pkg>.rb` override (1h
edge-cached) and falling back to convention `github.com/hop-top/<pkg>`
when no override exists. For `git`, the convention fallback is what
runs — no override formula is needed because the bare-name slot
already points at this repo.

Live probe (run today):

```sh
$ curl -sSL 'https://hop.top/git?go-get=1' | grep -i 'go-import\|go-source'
<meta name="go-import" content="hop.top/git git https://github.com/hop-top/git">
<meta name="go-source" content="hop.top/git https://github.com/hop-top/git https://github.com/hop-top/git/tree/main{/dir} https://github.com/hop-top/git/blob/main{/dir}/{file}#L{line}">
```

Both the `go-import` and `go-source` meta tags resolve to this
repo — the resolver is live and the mapping is correct.

### Install commands

```sh
go install hop.top/git@latest                       # highest semver
go install hop.top/git@git-hop/v0.1.0-alpha.2       # new prefixed tag
go install hop.top/git@v0.1.0-alpha.1               # legacy bare tag (preserved)
```

The legacy `v0.1.0-alpha.1` tag stays in the repo — proxy.golang.org
has it cached and anyone pinned to it keeps working. New releases
ship under the `git-hop/v...` prefix from
`git-hop/v0.1.0-alpha.2` forward.

### Post-merge verification

The three install probes below MUST be run AFTER the first
`git-hop/v0.1.0-alpha.2` tag actually exists in the repo (i.e.,
after the release-please bot ships its first release PR merge).
They're deferred from this track because the new prefixed tag
hasn't been cut yet:

```
GOPATH=$(mktemp -d) go install hop.top/git@v0.1.0-alpha.1            # legacy bare tag
GOPATH=$(mktemp -d) go install hop.top/git@git-hop/v0.1.0-alpha.2    # new prefixed tag
GOPATH=$(mktemp -d) go install hop.top/git@latest                    # resolves to highest semver
```

If `@latest` resolves to the legacy bare tag instead of the new
prefixed one, the proxy's SemVer ordering is reading the bare tag
as higher — escalate per the SKILL's vanity-imports override
path: add a `<pkg>.rb` formula in `hop-top/homebrew-tap` only as a
last resort (the convention fallback is what's expected here).
The more likely fix is to wait for the proxy to re-index after the
new tag lands.

### Binary version string

The Makefile's `VERSION` ldflag uses
`git describe --tags --always --dirty`. With the new tag shape,
`git describe` returns either:

- `git-hop/v0.1.0-alpha.2` on the tag itself, or
- `git-hop/v0.1.0-alpha.2-N-gSHA` between tags (N commits since the
  tag, current SHA),

and that string gets stamped into the binary as `main.version`.
End users see it via `git-hop --version`. The new shape reads
slightly longer than the legacy bare `v0.1.0-alpha.1-...`, but
proxy.golang.org consumers see the same value via the module's
own tag — no functional change, just docs to set expectations.

### Deep dive

Resolver internals, override mechanics, and edge cases:
`~/.w/ideacrafterslabs/dotgithub/references/concepts/vanity-imports.md`.

## References

- Skill: `~/.w/ideacrafterslabs/dotgithub/SKILL.md`
- Quick-start: `~/.w/ideacrafterslabs/dotgithub/references/quick-start.md`
- Single-language repo: `~/.w/ideacrafterslabs/dotgithub/references/how-to/single-language-repo.md`
- Prerelease channel: `~/.w/ideacrafterslabs/dotgithub/references/how-to/prerelease-channel.md`
- Preflight check: `~/.w/ideacrafterslabs/dotgithub/references/how-to/add-preflight.md`
- Retrigger failed publish: `~/.w/ideacrafterslabs/dotgithub/references/how-to/retrigger-failed-publish.md`
- Vanity imports concept: `~/.w/ideacrafterslabs/dotgithub/references/concepts/vanity-imports.md`
- Ship binaries (if revisiting): `~/.w/ideacrafterslabs/dotgithub/references/how-to/ship-binaries.md`
