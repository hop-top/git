# Contributing to git-hop

Org-wide policy (Conventional Commit types, release model, review sign-off) lives in
[hop-top/.github `CONTRIBUTING.md`](https://github.com/hop-top/.github/blob/main/CONTRIBUTING.md).
Code of conduct and security reporting are the org defaults:
[CODE_OF_CONDUCT.md](https://github.com/hop-top/.github/blob/main/CODE_OF_CONDUCT.md),
[SECURITY.md](https://github.com/hop-top/.github/blob/main/SECURITY.md).
This file covers only what is specific to this repository.

## Getting started

1. Fork + clone
2. Branch: `git checkout -b feat/my-change`
3. Change + tests
4. Run the CI steps locally (below)
5. Push; open a PR (fill in `.github/PULL_REQUEST_TEMPLATE.md`)

## Toolchain

- Go: version from the `go` directive in `go.mod` (CI uses `go-version-file: go.mod`)
- git on `PATH` (tests drive real local git)
- `staticcheck` for `make lint`: `go install honnef.co/go/tools/cmd/staticcheck@latest`
- Optional: Docker + Compose (docker e2e tier only), `lychee` (`make lint-links`)

## Build

```sh
make build     # ./git-hop, version ldflags
make install   # build, then copy to $GOBIN (default: $(go env GOPATH)/bin)
make fmt       # go fmt ./...
make lint      # go vet + staticcheck
```

## Tests

CI (`.github/workflows/ci.yml`) runs, in order:

```sh
gofmt -l .                                  # must print nothing
go build ./...
go vet ./...
staticcheck ./...
go test -timeout 300s -coverprofile=coverage.out ./...
```

Plus `go build .` on Linux, macOS, Windows.

`make test` covers `./internal/...` only. Changes under `cmd/` or `test/` need the full tree:

```sh
go test ./...                                                     # full suite
go test -run TestRemoveCommand_PartialFailureHandling ./cmd/...   # single test
go test -count=1 ./internal/hop/...                               # bypass test cache
```

Docker e2e tier sits behind the `dockere2e` build tag; not PR-blocking. Runs nightly, on manual
dispatch, or on a PR labelled `needs:docker-tests` (`.github/workflows/dockere2e.yml`). Locally:

```sh
go test -tags dockere2e -timeout 20m ./test/e2e/...
```

Tiers, isolation rules, xrr cassettes: [docs/testing.md](docs/testing.md).

## Conventions

`git hop` is a git subcommand; commands must behave like git porcelain:

- results on stdout; progress, hints, warnings on stderr with lowercase `hint:`, `warning:`,
  `error:`, `fatal:` prefixes
- exit codes: `0` success, `1` operation failure, `128` fatal git/repo error, `129` usage error
- git-standard flag names (`-n/--dry-run`, `-q/--quiet`, `-f/--force`, `--porcelain`)
- tunables via `git config hop.<command>.<key>`
- plain ASCII output; no emoji
- external deps behind interfaces (`git.GitInterface`, `afero.Fs`) so tests inject fakes
- files under ~500 LOC

## Commits + releases

Conventional Commits per the
[org policy](https://github.com/hop-top/.github/blob/main/CONTRIBUTING.md#conventional-commits).
Repo addition: custom `hop:` type, rendered in the "Interhop" changelog section.

Releases: release-please, bare `vX.Y.Z` tags, no manual tags, no hand edits to `CHANGELOG.md`
or `.github/version.json`. Full loop: [docs/release.md](docs/release.md).
