# app-go

A minimal Go HTTP service that responds.

This repository builds, tests and releases the binary. Containerization and
deployment live in a separate repository,
[app-deploy](https://github.com/ElAntagonista/app-deploy).

## Run locally

```sh
go run ./cmd/app                         # listens on :8080
curl localhost:8080                      # Hello from Mondoo Engineer!
curl localhost:8080/healthz              # ok
```

## Decisions log

### Project structure

```
cmd/app/           entrypoint: env config, signal handling, wiring
internal/server/   HTTP handler, server timeouts, graceful shutdown
```

- **`cmd/` + `internal/`, the standard Go service layout.** `main` only wires
  things together; the behaviour lives in `internal/server`, where it can be
  tested. `internal/` also means no other module can import it, so there is no
  public API to maintain.
- **Binary name `app`.** It comes from `cmd/app`, so `go build ./cmd/app` and
  the release assets share one name without extra flags.
- **Manual constructor wiring, no DI framework.** `server.New(addr, logger)`
  takes its only two dependencies explicitly; a container library would add a
  dependency and indirection for no benefit at this size.
- **Standard library only.** No third-party modules means nothing for
  dependency scanners to flag and nothing to keep patched.
- **Tests kept proportionate.** One unit test for the routes and one functional
  test that runs the server over real TCP and checks graceful shutdown.

### Runtime contract

The deployment repo and Kubernetes depend on these behaviours, so they are
fixed here deliberately:

- **Port `$PORT`, default `8080`.** A non-privileged port lets the container
  run as a non-root user.
- **`GET /healthz`** returns `200 ok` for liveness and readiness probes.
- **Graceful shutdown on `SIGTERM`.** The server stops accepting connections
  and drains in-flight requests within 10s, inside Kubernetes' default 30s
  termination grace period.
- **Bounded server timeouts** (read header, read, write, idle) so slow clients
  cannot hold connections open indefinitely (slowloris).
- **JSON logs on stdout**, ready for any log collector (12-factor).

### CI pipeline

`.github/workflows/ci.yml` runs on every push to `main` and on every pull
request.

- **SAST: semgrep** with the `p/golang` registry ruleset, in the official
  container, pinned to 1.181.0 (the same version as `make sast`). No Semgrep
  account or token needed. Findings fail the build (`--error`).
- **Lint: golangci-lint** v2.14.0 (the same version as `make lint`) with the
  curated `.golangci.yml`: correctness, security and resource-leak linters
  rather than style rules. `setup-go` reads the toolchain from `go.mod`, so CI
  lints with the pinned compiler.
- **Test & build** runs only after both gates pass (`needs: [sast, lint]`):
  `make test` (race detector, shuffled order), then
  `goreleaser release --snapshot`, the full release pipeline minus publishing.
  Binaries and `checksums.txt` are kept as a 7-day artifact; `make snapshot`
  runs the same build locally.

  **Why GoReleaser** rather than a hand-written `GOOS`/`GOARCH` build matrix:
  - *One definition of the release.* Targets, build flags, version stamping,
    asset names and checksums live in `.goreleaser.yaml`, which is versioned
    and reviewed like code, instead of being spread across workflow shell
    steps.
  - *The release is rehearsed on every commit.* CI runs the same config in
    snapshot mode, so a cross-compile break or a config mistake shows up on
    the commit that causes it, not when a tag is pushed.
  - *A smaller release workflow.* Publishing becomes a single step, so the
    tag-triggered workflow has less code to get wrong.
  - *Room to grow.* SBOMs, cosign signing and changelogs are built-in options
    rather than extra scripting.
  - *Ecosystem standard.* It is the de facto tool for Go release automation,
    and the OSS edition (MIT) covers everything used here.
  - *Trade-off:* one more pinned tool, and the cross-compile mechanics sit
    behind its config instead of in plain workflow YAML.
- **Least privilege.** The workflow token is read-only (`contents: read`).
- **Concurrency.** A new push to a pull request cancels its in-flight run.
  Runs on `main` always finish, so every commit on `main` gets a result.

### Release

`.github/workflows/release.yml` runs when a semver tag is pushed:

1. **Test** with a read-only token. The release re-runs the tests instead of
   trusting an earlier CI run.
2. **Build & publish** (`contents: write`): `goreleaser release` builds all
   four targets and publishes the GitHub Release with the binaries,
   `checksums.txt` and generated release notes. The write token is never
   exposed to test code, and `persist-credentials: false` keeps it out of
   `.git/config`.

- **Strict tag filter** `v[0-9]+.[0-9]+.[0-9]+`. Only semver tags publish; a
  stray tag such as `test` or `v1` cannot.
- **Asset names are a contract with app-deploy:**
  `app_v<version>_<os>_<arch>` plus `checksums.txt` (SHA-256). Changing them
  breaks the deployment repo.
- **Static, reproducible binaries.** `CGO_ENABLED=0` lets the Linux builds run
  on a distroless/static base image; `-trimpath` and commit-timestamp file
  times make rebuilds byte-identical.
- **Version stamping.** The tag is injected via `-X main.version` and logged
  at startup, so a running pod can be traced back to its release.
- **macOS** (amd64 + arm64) is included to show cross-compilation. The
  binaries are not Apple-notarized, so a browser download needs
  `xattr -d com.apple.quarantine`.
- **SBOM per binary.** GoReleaser runs syft on each binary and attaches
  `app_v<version>_<os>_<arch>.sbom.json` (SPDX 2.3 JSON) to the release; the
  SBOMs are also listed in `checksums.txt`. SPDX is GoReleaser's default, an
  ISO standard, and what GitHub's dependency graph uses. SBOMs are published
  only with releases; CI generates them in its snapshot build so a broken SBOM
  step fails on the commit that caused it, not on release day.
- **Releases are never cancelled.** A second tag queues behind a running
  release instead of interrupting it mid-publish.

#### Cutting a release

```sh
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

Release tags are **annotated**, so they record the tagger, the date and a
message. **Signed tags (`git tag -s`) would be best practice for supply-chain
trust:** an annotated tag's tagger is self-declared and can be forged by
anyone with push access, while a signature proves who cut the release and
shows as "Verified" on GitHub.

### Cross-repo dispatch

After each successful release, `.github/workflows/dispatch.yml` tells
[app-deploy](https://github.com/ElAntagonista/app-deploy) to package it:

```
tag v1.2.3 → Release workflow → workflow_run → Dispatch release
           → repository_dispatch (new-release, {"version": "v1.2.3"}) → app-deploy
```

- **A separate workflow, triggered by `workflow_run`** on the Release workflow
  completing successfully. The cross-repo token is only available to this
  small job (no checkout, no third-party actions, `permissions: {}`), never to
  the job that builds and publishes the release. The dispatch can also be
  re-run on its own.
- **Not `on: release: published`.** It looks like the obvious trigger but
  never fires here: GitHub does not start workflows from events caused by
  `GITHUB_TOKEN`, and GoReleaser publishes with it. Making it fire would mean
  publishing with a stronger personal token inside the release job.
- **Other options rejected:** a dispatch step at the end of `release.yml`
  (exposes the token to the release job and can't be re-run alone); calling
  app-deploy's workflow as a reusable workflow (it would execute in app-go,
  defeating the two-repo setup); app-deploy polling for new releases (latency,
  and not an explicit hand-off).
- **`repository_dispatch` rather than `workflow_dispatch`.** app-go announces
  an event; it does not need to know which workflow file in app-deploy
  handles it.
- **The version is validated on both sides** against strict semver, so a
  malformed payload is rejected before anything runs.
- **Credential: `DEPLOY_REPO_PAT`**, a fine-grained token scoped to
  app-deploy only, with *Contents: Read and write* (the minimum the dispatch
  API accepts) and a 30-day expiry. The default `GITHUB_TOKEN` cannot reach
  another repository. In production this would be a GitHub App installation
  token minted per run (`actions/create-github-app-token`): short-lived, not
  tied to a person, no manual rotation.
