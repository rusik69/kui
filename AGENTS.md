# AGENTS.md

Guidance for coding agents (and humans) working on kui. It complements
`README.md`, which documents the tool for users.

## What kui is

A lightweight, local Kubernetes web UI in one Go binary: browse resources,
tail logs, exec into pods, edit/delete resources, and port-forward. Go
backend (`client-go` + dynamic/discovery clients), HTMX + vanilla JS
frontend, server-rendered `html/template` with frontend assets embedded via
`go:embed`. A native webview (webview_go) wraps the same UI in desktop mode.
There is no Node/npm production build step; Node.js 22 + Playwright are used
only for browser tests.

## Layout

```
cmd/kui/            main package: flags, signal handling, webview window
internal/k8s/       Kubernetes layer: client/context manager, watches, logs,
                    exec, port-forward, debug pods, Rancher source, metrics
internal/settings/  settings.json persistence (Rancher + kubeconfig dir)
internal/web/       HTTP server: handlers, templates, static assets (embedded)
tests/ui/           Playwright browser tests (test-only tooling)
```

Frontend assets live in `internal/web/static` and `internal/web/templates`
and are embedded at compile time — after editing them, rebuild or re-run
tests to see the change.

## Commands

```sh
make ci        # gofmt check, go vet, golangci-lint, race tests + coverage, build
make test      # race-enabled Go tests only
make test-ui   # build kui, run Playwright suite against a local API fixture
make fmt-fix   # apply gofmt
make tidy      # go mod tidy
```

Go version comes from `go.mod` (1.26.5). Linux builds need GTK 3 and
WebKitGTK 4.0 dev packages; macOS needs none; Windows needs the WebView2
runtime. Lint version is pinned in the `Makefile`.

Run a focused test:

```sh
go test ./internal/web -run TestKubeconfigDirValidation -race
```

The browser suite needs `npm ci` and `npx playwright install chromium` once
(`--with-deps` on Ubuntu). It never touches a real cluster.

## Conventions

- Keep code `gofmt`-clean; `make ci` fails on unformatted files.
- Every exported identifier carries a doc comment explaining not just what it
  does but the non-obvious constraints behind it (see `internal/settings`
  for the expected style).
- Tests live next to the code as `_test.go` files in the same package; table
  tests and `httptest` servers are the norm. Regression tests accompany bug
  fixes.
- Commit messages follow Conventional Commits (`feat:`, `fix:`,
  `feat(ui):`, ...).
- Treat all credentials (kubeconfig contents, Rancher tokens, settings.json)
  as secrets: never log them, never return tokens from API responses, keep
  files at 0600 and directories at 0700.
- kui is a local, single-user tool by design — do not add auth, TLS, or
  multi-user features (see the Non-goals section of `README.md`).

## Release process

Releases are tag-driven and handled with the GitHub CLI:

1. Ensure `main` is green and pushed: `make ci`, then `git push origin main`.
2. Tag the next version following semver — feature work bumps the minor,
   fixes the patch (e.g. `v0.6.2` → `v0.7.0`):

   ```sh
   git tag -a vX.Y.Z -m "kui vX.Y.Z" && git push origin vX.Y.Z
   ```

3. Create the GitHub release, which triggers `.github/workflows/release.yml`:

   ```sh
   gh release create vX.Y.Z --title "kui vX.Y.Z" --verify-tag --notes-file notes.md
   ```

4. The workflow builds single-file binaries for Linux amd64/arm64, macOS
   Intel/Apple Silicon, and Windows amd64, smoke-tests each one, then uploads
   them with SHA-256 checksums, an SPDX SBOM (`kui.spdx.json`), and build
   provenance attestations. Check `gh run list` until the run finishes.
