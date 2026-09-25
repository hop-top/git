<!-- Adapted from the hop-top/.github template; maintained in this repo. -->
<!-- Title = Conventional Commit. No (#N) suffix, no tracker refs. -->

## What

<!-- Root cause / gap, then what this change does about it. -->

## Verification

<!-- Evidence, not claims: commands run + relevant output/numbers. -->

## Checks

- [ ] Conventional Commit title; no tracker/internal refs anywhere in the diff
- [ ] Tests cover the change; CI steps green locally (`gofmt -l .`, `go vet ./...`, `staticcheck ./...`, `go test ./...`)
- [ ] Command/flag/output change? Follows git porcelain conventions (streams, exit codes, flag names)
- [ ] Docs updated (README / docs/) if behavior changed
- [ ] Breaking change → `!` in title + migration note above
