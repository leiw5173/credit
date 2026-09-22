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
| Reviewed source baseline | `6083983e0b92f4f15bedd9a8ad714848ff07d82b` |
| Parent gitlink | A later documentation-only fork commit; verify with `git submodule status` |
| Fork and upstream `master` at source verification | `6083983e0b92f4f15bedd9a8ad714848ff07d82b` |
| License SHA-256 | `f727e693aaf6c0181ea65fa75e233914b459fa8110bfacc72a7a6ad87380fda0` |

`origin` is configured and points only to the authorized fork. On 2026-09-22,
the approved local `upstream` remote was configured exactly as
`https://github.com/linux-do/credit`; its required ref probe succeeded:

```text
$ git -C credis ls-remote upstream HEAD refs/heads/main refs/heads/master
6083983e0b92f4f15bedd9a8ad714848ff07d82b	HEAD
c849550731fdc7fd3faa9559ff8d4496e6e910a4	refs/heads/main
6083983e0b92f4f15bedd9a8ad714848ff07d82b	refs/heads/master
```

The reviewed application source remains upstream `master`/`HEAD`
`6083983…`; `main` is a distinct ref and was not incorporated. The earlier
GitHub API equality evidence is retained as historical source-verification
context, not as a substitute for this local remote check.

## Re-evaluated drift from written design baseline

The written design baseline is two commits behind the reviewed source baseline:
`2d09c890e06e7c12906f1b641e8248646816590a`.

```text
$ git diff --name-status 2d09c890e06e7c12906f1b641e8248646816590a..6083983e0b92f4f15bedd9a8ad714848ff07d82b
M       internal/apps/redenvelope/routers.go
M       internal/apps/redenvelope/utils.go
A       internal/apps/redenvelope/utils_test.go

$ git diff --stat 2d09c890e06e7c12906f1b641e8248646816590a..6083983e0b92f4f15bedd9a8ad714848ff07d82b
 internal/apps/redenvelope/routers.go    |  2 +-
 internal/apps/redenvelope/utils.go      |  5 +++++
 internal/apps/redenvelope/utils_test.go | 32 ++++++++++++++++++++++++++++++++
 3 files changed, 38 insertions(+), 1 deletion(-)

$ git diff --check 2d09c890e06e7c12906f1b641e8248646816590a..6083983e0b92f4f15bedd9a8ad714848ff07d82b
[exit 0; no output]
```

The two commits are `10c6643` (`fix(redenvelope): use local midnight for
daily limit`) and merge commit `6083983`. The drift is limited to internal
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

$ git -C credis status --short
[exit 0; no output, clean]

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

$ node -p "require('./frontend/package.json').dependencies.next + ' / ' + require('./frontend/package.json').dependencies.react + ' / ' + require('./frontend/package.json').dependencies['react-dom']"
16.1.1 / 19.2.3 / 19.2.3
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

The first production-build attempt failed because the environment could not
retrieve a Google-hosted font:

```text
next/font: error:
Failed to fetch `Geist Mono` from Google Fonts.
ELIFECYCLE Command failed with exit code 1.
```

A covering rerun on 2026-09-22 passed lint and the production build with the
same installed dependencies:

```text
$ (cd credis/frontend && pnpm lint && pnpm build)
> linux-do-credit@1.3.9 lint
> eslint

> linux-do-credit@1.3.9 build
> next build
▲ Next.js 16.1.1 (Turbopack)
✓ Compiled successfully
✓ Generating static pages using 9 workers (27/27)
[exit 0]
```

The initial network failure remains part of the baseline evidence; the later
successful build is the current covering result.

Docker CLI 29.8.0, Docker Compose v5.5.1, and the Docker daemon 29.8.0 are
available. The initial runtime attempt was incomplete because private-network
creation was then denied; it started no dependency containers or roles. That
historical limitation is superseded by the successful isolated runtime evidence
in the Round 3 section below. The fresh local-only images used for the completed
check were the digest-resolved images:

- PostgreSQL 18.6: `postgres@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650`
- Redis 7.4.11: `redis@sha256:c6eabf748fc7a61dbb5a705c78bcf3d6377b1127a97d0ce965c11c44ba46896f`

## Reproduction status

The reviewed application source is `6083983…`; the parent gitlink intentionally
points to a later documentation-only Fork commit. Before authorized publication,
a fresh `git submodule update --init --recursive` was correctly recorded as an
expected failure because the documentation object was local-only. Round 3
published `8e9ade79…` and then proved that a genuinely fresh parent clone
fetched that exact gitlink over the configured Fork HTTPS origin. The detailed
command output is preserved below.

For any later documentation-only gitlink update, verify both the actual gitlink
and reviewed source baseline separately:

```bash
git submodule update --init --recursive
git submodule status
git -C credis rev-parse HEAD
git -C credis rev-parse origin/master
# The gitlink is the published documentation commit; origin/master remains the
# reviewed application source baseline 6083983e0b92f4f15bedd9a8ad714848ff07d82b.
```

## Round 3 completion evidence (2026-09-22)

### Published gitlink and genuinely fresh initialization

The previously local-only documentation commit was published only to the
authorized non-default Fork branch:

```text
$ git -C credis push origin 8e9ade79a7b471280193e4aeabc8306da00601c9:refs/heads/docs/credis-upstream-baseline
To https://github.com/leiw5173/credit.git
 * [new branch] 8e9ade79a7b471280193e4aeabc8306da00601c9 -> docs/credis-upstream-baseline
```

A new disposable parent clone was then made without a submodule reference or
local Fork-object reuse. Its normal HTTPS submodule initialization fetched and
checked out the exact parent gitlink:

```text
$ git submodule update --init --recursive
Submodule 'credis' (https://github.com/leiw5173/credit.git) registered for path 'credis'
Cloning into '<fresh-parent>/credis'...
Submodule path 'credis': checked out '8e9ade79a7b471280193e4aeabc8306da00601c9'

$ git submodule status
 8e9ade79a7b471280193e4aeabc8306da00601c9 credis (remotes/origin/docs/credis-upstream-baseline)
```

This is fresh-clone reproducibility evidence for the documentation gitlink.
The parent repository itself was not pushed.

### Current baseline commands

The required immutable Go image is Go 1.26.8. Its raw read-only test command
was rerun against this checkout and remains a genuine failure because the test
package opens an absent `config.yaml`:

```text
$ docker run --rm --mount type=bind,src=<checkout>/credis,dst=/src,readonly -w /src \
  golang@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d go test ./...
...
2026/09/21 23:52:37 [Config] read config failed: open config.yaml: no such file or directory
FAIL    github.com/linux-do/credit/internal/apps/redenvelope
FAIL
[exit 1]
```

The current genuine frontend command passed:

```text
$ (cd credis/frontend && pnpm install --frozen-lockfile && pnpm lint && pnpm build)
Lockfile is up to date
> eslint
> next build
✓ Compiled successfully
✓ Generating static pages using 9 workers (27/27)
[exit 0]
```

`pnpm` reported ignored build scripts for `core-js`, `sharp`, and
`unrs-resolver`; Next.js also warned that it inferred a workspace root from an
unrelated parent lockfile. Neither warning prevented lint or build completion.

### Isolated API, worker, and scheduler runtime

A new local-only Docker network named `credis-task1-net` ran fresh no-volume
containers from the pinned images recorded above. A disposable `config.yaml`
used only `credis-task1-postgres`, `credis-task1-redis`, database
`credis_task1`, placeholder OAuth fields, and disabled S3. The source checkout
was mounted read-only into each Go 1.26.8 role container; the config was copied
only into its private working directory.

- API `/api/v1/health` and `/api/v1/ready` returned `{"error_msg":"","data":null}`.
- Worker and scheduler probe endpoints on their configured ports returned the
  same success response for both health and readiness checks.
- The scheduler's temporary once-per-minute, empty-database
  `dispute:auto_refund_expired` scan was processed by the worker. It recorded
  `[TaskMiddleware] 任务处理完成 Type: dispute:auto_refund_expired` with 25 ms
  and 3 ms latencies; the query found zero disputes, so no external or
  production data was used.

The Docker Desktop host-published IPv4 port reset while the API's `:8000`
listener was IPv6 wildcard; direct in-container HTTP probes passed. This is a
host-forwarding observation, not a source change or an application readiness
failure.

After evidence capture, `credis-task1-api`, `credis-task1-worker`,
`credis-task1-scheduler`, `credis-task1-postgres`, `credis-task1-redis`, the
`credis-task1-net` network, and the disposable clone/configuration directories
were removed. No production service, data, parent branch, or deployment was
used.
