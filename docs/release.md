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
   - pushes the bare tag `vX.Y.Z-alpha.N`,
   - creates the matching GitHub Release,
   - proxy.golang.org picks the tag up within minutes.

4. **Install the new version.**

   ```sh
   go install hop.top/git@vX.Y.Z-alpha.N   # pin to the exact tag
   go install hop.top/git@latest           # resolve to highest semver
   ```

   See [Tag shape](#tag-shape) for why tags are bare `vX.Y.Z` and
   not the `<component>/vX.Y.Z` shape used by other hop-top repos.

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

1. Branch from the release tag: `git switch -c hotfix-X.Y.Z vX.Y.Z-alpha.N`.
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
runbook
[`references/how-to/retrigger-failed-publish.md`](https://github.com/hop-top/.github/blob/main/references/how-to/retrigger-failed-publish.md).
Do NOT delete + force-push tags by hand — proxy.golang.org caches
tag content and re-pushing the same tag with different content
produces ambiguous module versions in the wild.

## Tag shape

Tags here are bare `vX.Y.Z-alpha.N` — not the `<component>/vX.Y.Z`
shape the hop-top org uses elsewhere. The deviation is deliberate
and load-bearing: the Go module proxy at `proxy.golang.org`
rejects `/` characters in version queries against a root module.
A `git-hop/vX.Y.Z` tag would parse as a request for a submodule
`hop.top/git/git-hop`, which doesn't exist:

```
$ go install hop.top/git@git-hop/v0.1.0-alpha.1
go: invalid version: ... "git-hop/v0.1.0-alpha.1" invalid:
    disallowed version string
```

The `<component>/v<version>` convention exists for `publish.yml`'s
`tags: ['*/v*']` glob — but this repo doesn't ship `publish.yml`
(see [Why there is no publish.yml](#why-there-is-no-publishyml)).
With the convention's beneficiary absent, paying its cost (broken
`go install`) is a net loss. release-please's
`include-component-in-tag: false` keeps tags bare and `go install`
working.

Submodule repos (module path like `hop.top/foo/bar` instead of
`hop.top/foo`) can use the prefixed shape — the proxy treats
`foo/bar/vX.Y.Z` as `module=foo/bar, version=vX.Y.Z`. Root-module
repos like this one cannot. See the SKILL's
[`references/concepts/vanity-imports.md`](https://github.com/hop-top/.github/blob/main/references/concepts/vanity-imports.md)
for the resolver mechanics.

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
release-please-action pushes vX.Y.Z-alpha.N
    ↓
GitHub Release is created from the tag
    ↓
proxy.golang.org indexer picks the tag up (≤ a few minutes)
    ↓
go install hop.top/git@vX.Y.Z-alpha.N  works
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
[`references/how-to/ship-binaries.md`](https://github.com/hop-top/.github/blob/main/references/how-to/ship-binaries.md)
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
go install hop.top/git@latest          # highest semver
go install hop.top/git@vX.Y.Z-alpha.N  # pin to exact tag
```

The legacy `v0.1.0-alpha.1` tag stays installable —
proxy.golang.org has it cached. New releases ship under the same
bare `vX.Y.Z-alpha.N` shape, linear from there.

### Post-merge verification

Run after the first release-please-bot PR merges and the new tag
exists. Verifies the proxy indexed the new tag and `@latest`
resolves correctly:

```sh
curl -s 'https://proxy.golang.org/hop.top/git/@v/list'
# Expect: vX.Y.Z-alpha.N for every released tag (including legacy v0.1.0-alpha.1)

GOPATH=$(mktemp -d) go install hop.top/git@v0.1.0-alpha.1   # legacy still installs
GOPATH=$(mktemp -d) go install hop.top/git@latest           # resolves to highest semver
```

If `@latest` doesn't resolve to the newest tag, wait a few minutes
and retry — the proxy indexer is asynchronous. Persistent
mis-resolution would indicate a SemVer ordering issue (a "ghost
version" higher than the intended tag); the fix is the
`Release-As:` base-jump pattern documented in
[`references/troubleshooting/go.md` § Ghost versions][trbl-go]
in the SKILL.

[trbl-go]: https://github.com/hop-top/.github/blob/main/references/troubleshooting/go.md

### Binary version string

The Makefile's `VERSION` ldflag uses
`git describe --tags --always --dirty`. With bare-tag shape,
`git describe` returns:

- `vX.Y.Z-alpha.N` on the tag itself, or
- `vX.Y.Z-alpha.N-K-gSHA` between tags (K commits since the tag,
  current short SHA),

and that string gets stamped into the binary as `main.version`.
End users see it via `git-hop --version`. No functional change
versus the legacy flow — same shape, same stamping, same proxy
semantics.

### Deep dive

Resolver internals, override mechanics, and edge cases:
[`references/concepts/vanity-imports.md`](https://github.com/hop-top/.github/blob/main/references/concepts/vanity-imports.md).

## References

All paths below resolve to `https://github.com/hop-top/.github/blob/main/`:

- [`SKILL.md`](https://github.com/hop-top/.github/blob/main/SKILL.md)
- [`references/quick-start.md`](https://github.com/hop-top/.github/blob/main/references/quick-start.md)
- [`references/how-to/single-language-repo.md`](https://github.com/hop-top/.github/blob/main/references/how-to/single-language-repo.md)
- [`references/how-to/prerelease-channel.md`](https://github.com/hop-top/.github/blob/main/references/how-to/prerelease-channel.md)
- [`references/how-to/add-preflight.md`](https://github.com/hop-top/.github/blob/main/references/how-to/add-preflight.md)
- [`references/how-to/retrigger-failed-publish.md`](https://github.com/hop-top/.github/blob/main/references/how-to/retrigger-failed-publish.md)
- [`references/concepts/vanity-imports.md`](https://github.com/hop-top/.github/blob/main/references/concepts/vanity-imports.md)
- [`references/how-to/ship-binaries.md`](https://github.com/hop-top/.github/blob/main/references/how-to/ship-binaries.md) (if revisiting `publish.yml`)
- [`references/troubleshooting/go.md`](https://github.com/hop-top/.github/blob/main/references/troubleshooting/go.md) (Go-specific failures, ghost versions)
