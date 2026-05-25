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

## References

- Skill: `~/.w/ideacrafterslabs/dotgithub/SKILL.md`
- Quick-start: `~/.w/ideacrafterslabs/dotgithub/references/quick-start.md`
- Single-language repo: `~/.w/ideacrafterslabs/dotgithub/references/how-to/single-language-repo.md`
- Prerelease channel: `~/.w/ideacrafterslabs/dotgithub/references/how-to/prerelease-channel.md`
- Preflight check: `~/.w/ideacrafterslabs/dotgithub/references/how-to/add-preflight.md`
- Retrigger failed publish: `~/.w/ideacrafterslabs/dotgithub/references/how-to/retrigger-failed-publish.md`
- Ship binaries (if revisiting): `~/.w/ideacrafterslabs/dotgithub/references/how-to/ship-binaries.md`
