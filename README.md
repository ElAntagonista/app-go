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
