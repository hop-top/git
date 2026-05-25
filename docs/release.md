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

## References

- Skill: `~/.w/ideacrafterslabs/dotgithub/SKILL.md`
- Quick-start: `~/.w/ideacrafterslabs/dotgithub/references/quick-start.md`
- Prerelease channel: `~/.w/ideacrafterslabs/dotgithub/references/how-to/prerelease-channel.md`
- Preflight check: `~/.w/ideacrafterslabs/dotgithub/references/how-to/add-preflight.md`
- Retrigger failed publish: `~/.w/ideacrafterslabs/dotgithub/references/how-to/retrigger-failed-publish.md`
