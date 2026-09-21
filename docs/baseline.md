# Credis upstream baseline

## Purpose

This document fixes the source baseline for the Credis P0 work before any
behavioural change. The parent repository consumes the authorized independent
fork as a submodule; it must not silently follow the upstream default branch.

## Fixed source

| Item | Value |
| --- | --- |
| Parent submodule path | `credis/` |
| Authorized fork (origin) | `https://github.com/leiw5173/credit.git` |
| Intended upstream | `https://github.com/linux-do/credit` |
| Fixed commit | `6083983e0b92f4f15bedd9a8ad714848ff07d82b` |
| Fork and upstream `master` at verification | `6083983e0b92f4f15bedd9a8ad714848ff07d82b` |
| License SHA-256 | `f727e693aaf6c0181ea65fa75e233914b459fa8110bfacc72a7a6ad87380fda0` |

`origin` is configured and points only to the authorized fork. Adding the
local `upstream` remote was denied by the execution environment. Therefore
`git -C credis ls-remote upstream HEAD refs/heads/main refs/heads/master`
currently fails with `fatal: 'upstream' does not appear to be a git
repository`. This is an explicit incomplete gate, not a passing upstream
check. The fork/upstream commit equality above was independently confirmed
through the GitHub API.

## Re-evaluated drift from written design baseline

The written design baseline is two commits behind this fixed source:
`3c0ff2684ad3fde49634d769833f2c4e35b33334`.

```text
$ git diff --name-status 3c0ff2684ad3fde49634d769833f2c4e35b33334..6083983e0b92f4f15bedd9a8ad714848ff07d82b
M       internal/apps/redenvelope/routers.go
M       internal/apps/redenvelope/utils.go
A       internal/apps/redenvelope/utils_test.go

$ git diff --stat 3c0ff2684ad3fde49634d769833f2c4e35b33334..6083983e0b92f4f15bedd9a8ad714848ff07d82b
 internal/apps/redenvelope/routers.go    |  4 ++--
 internal/apps/redenvelope/utils.go      |  5 +++++
 internal/apps/redenvelope/utils_test.go | 32 ++++++++++++++++++++++++++++++++
 3 files changed, 39 insertions(+), 2 deletions(-)
```

The two commits are `10c6643` (`fix(redenvelope): use local midnight for
daily limit`) and merge commit `6083983`. `git diff --check` reports no
whitespace errors in this range. The drift is limited to internal
red-envelope daily-window handling and its test; it is documented before this
baseline lock and is not absorbed as an unreviewed P0 change.

## Verification evidence

The following describes the actual local evidence, not an idealized result.

```text
$ git -C credis rev-parse HEAD
6083983e0b92f4f15bedd9a8ad714848ff07d82b

$ git -C credis branch -r
  origin/HEAD -> origin/master
  origin/feat/credis-p0
  origin/master

$ sha256sum credis/LICENSE
zsh: command not found: sha256sum

$ shasum -a 256 credis/LICENSE
f727e693aaf6c0181ea65fa75e233914b459fa8110bfacc72a7a6ad87380fda0  credis/LICENSE

$ go version (initial host PATH)
zsh: command not found: go

$ PATH=/usr/local/go/bin:$PATH go version (installed during verification)
go version go1.27.1 darwin/arm64

$ node --version
v26.4.0

$ pnpm --version
10.33.1
```

`go.mod` declares `go 1.26`; host Go 1.27.1 is recorded as a version mismatch,
not as Go 1.26 evidence. A digest-pinned Docker test was attempted without
modifying host PATH:

```text
$ docker run --rm --mount type=bind,src=<isolated-checkout>/credis,dst=/src,readonly -w /src \
  golang@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d go test ./...
Digest: sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d
...
2026/09/21 14:37:53 [Config] read config failed: open config.yaml: no such file or directory
FAIL    github.com/linux-do/credit/internal/apps/redenvelope
FAIL
```

The immutable image resolves `golang:1.26-bookworm` (Go 1.26.8). The raw host
run with `PATH=/usr/local/go/bin:$PATH go test ./...` has the same
`config.yaml` failure. Neither is a pass.

Frontend dependency installation and lint succeeded:

```text
$ (cd credis/frontend && pnpm install --frozen-lockfile && pnpm lint && pnpm build)
Lockfile is up to date, resolution step is skipped
Done in 26s using pnpm v10.33.1
> linux-do-credit@1.3.9 lint
> eslint
```

The production build then failed because the environment could not retrieve a
Google-hosted font, not because of a lint or source error:

```text
next/font: error:
Failed to fetch `Geist Mono` from Google Fonts.
ELIFECYCLE Command failed with exit code 1.
```

Docker CLI 29.8.0, Docker Compose v5.5.1, and the Docker daemon 29.8.0 are
available. API, worker, and scheduler runtime checks are **not passed**:
creating the required private Docker network was denied by the execution
environment. No PostgreSQL/Redis containers, application processes, health
requests, task invocations, or task logs were therefore created. The proposed
fresh local-only images had already been digest-resolved:

- PostgreSQL 18.6: `postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650`
- Redis 7.4.11: `redis@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f`

## Reproduction

```bash
git submodule update --init --recursive
test "$(git config -f .gitmodules submodule.credis.url)" = \
  "https://github.com/leiw5173/credit.git"
git -C credis rev-parse HEAD
# Expected fixed source: 6083983e0b92f4f15bedd9a8ad714848ff07d82b
```

Before treating this baseline as fully verified, an authorized local operator
must add the configured upstream remote and run the runtime checks against
fresh local PostgreSQL/Redis instances.
