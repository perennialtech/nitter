# Development

Nitter is a Go program. The entry point is `cmd/nitter`; everything else is
under `internal/`. Stylesheets are SCSS in `sass/`, compiled to
`public/css/style.css`.

## Gates

```bash
make check
```

This is what CI runs: `gofmt`, `go vet` and `go test ./...`. The Go tests
use local fakes and need no network access.

Every setting is defined and documented in code: config keys and
environment variables in `internal/config/options.go`, preferences in
`internal/prefs/prefs.go`. `make generate` writes `nitter.example.conf` and
`nitter.example.env` from them, and `make check` fails until those are
current.

## Building and running

```bash
make build
cp nitter.example.conf nitter.conf
./nitter
```

`make build` needs `sassc` for the stylesheets. Nitter reads account
sessions from `sessions.jsonl` (see the README); `nitter.example.env` lists
the variables that override both paths.

## Tests against a running instance

The Python tests in `tests/` run against a built binary, as CI does:

- `test_fork_smoke.py` and `test_media_socket_cleanup.py` need no Twitter
  credentials. Start `./nitter`, then run them with `NITTER_TEST_URL` set to
  its address and `NITTER_TEST_PID` to its process ID. The media test needs
  `pbs.twimg.com` resolving to `127.0.0.1`, which CI sets in `/etc/hosts`.
- `test_api_resource_cleanup.py` starts its own `./nitter` against a fake
  API.
- The rest are seleniumbase integration tests against live Twitter and need
  working sessions. Run them with `uv run pytest` from `tests/` while
  `./nitter` serves `localhost:8080`.

`go run ./cmd/test-sessions sessions.jsonl` checks each cookie session in a
sessions file with one request.

## Pinned dependencies

Everything fetched from outside the repo is pinned to an immutable
reference, with its release in a comment next to it:

- GitHub Actions by commit SHA (`uses: actions/checkout@<sha> # v6.1.0`).
- The Dockerfile's builder image by digest
  (`golang:1.27.4-alpine3.24@sha256:...`). `scratch` has no digest.
- Go modules by `go.sum`, and the Python dependencies of the tests and the
  session tools by `tests/uv.lock` and `tools/uv.lock`. After editing a
  `pyproject.toml`, run `uv lock` in its directory.

To update a pin, change the SHA or digest and its comment together. Resolve
a tag with `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` (dereference
annotated tags) or `docker buildx imagetools inspect <image>:<tag>`.
