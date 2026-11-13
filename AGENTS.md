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

## Building and running

```bash
make build
cp nitter.example.conf nitter.conf
./nitter
```

`make build` needs `sassc` for the stylesheets. Nitter reads account
sessions from `sessions.jsonl` (see the README). `NITTER_CONF_FILE` and
`NITTER_SESSIONS_FILE` override both paths.

## Tests against a running instance

The Python tests in `tests/` run against a built binary, as CI does:

- `test_fork_smoke.py` and `test_media_socket_cleanup.py` need no Twitter
  credentials. Start `./nitter`, then run them with `NITTER_TEST_URL` set to
  its address and `NITTER_TEST_PID` to its process ID. The media test needs
  `pbs.twimg.com` resolving to `127.0.0.1`, which CI sets in `/etc/hosts`.
- `test_api_resource_cleanup.py` starts its own `./nitter` against a fake
  API.
- The rest are seleniumbase integration tests against live Twitter and need
  working sessions. Run them with `poetry run pytest` from `tests/` while
  `./nitter` serves `localhost:8080`.

`go run ./cmd/test-sessions sessions.jsonl` checks each cookie session in a
sessions file with one request.
