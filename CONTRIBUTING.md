# Building and testing

Install Nim 2.2.12 or newer, libsass, and libpcre as described in the
[installation instructions](README.md#installation). From the repository root:

```bash
nimble install -y --depsOnly
nimble build -Y
nimble scss
nimble md
nim r tests/test_ssrf_1411.nim
nim r tests/test_formatters.nim
nim r tests/test_timeline.nim
```

The last three commands test media URL security, HTML text formatting, and
pinned tweet rendering without Twitter credentials.

## HTTP proxy regression tests

With Python 3 and the OpenSSL command installed, run:

```bash
python3 tests/test_http_proxy.py -v
```

The tests compile Nitter's HTTP pool and check plain HTTP and HTTPS CONNECT
requests, query strings, connection reuse, and updated headers against a local
proxy, including credentials in the proxy URL or `proxyAuth`. They use a
temporary trusted certificate and need no account sessions or internet access.

Docker builds run these tests with the image's pinned compiler before building
Nitter.

## Offline smoke tests

Start Redis or Valkey and a local Nitter instance using your `nitter.conf`.
These tests need Python 3 but no additional Python packages or valid Twitter
sessions; an empty `sessions.jsonl` file is sufficient. They check local pages,
generated assets, and all four Catppuccin themes without making Twitter requests:

```bash
NITTER_TEST_URL=http://127.0.0.1:8080 \
  python3 -m unittest discover -s tests -p test_fork_smoke.py -v
```

## CI and live integration tests

The [image workflow](.github/workflows/build-docker.yml) calls the reusable
[test workflow](.github/workflows/run-tests.yml) for pull requests to `master`
and before publishing images. It checks Nim 2.2.12 and devel, generates assets,
runs the security regression tests, and tests local pages and Catppuccin themes.

The upstream browser suite makes live Twitter requests and requires working
account sessions. To enable it in GitHub Actions, configure the repository's
`SESSIONS` Actions secret with the contents of `sessions.jsonl`, following the
upstream [session token guide](https://github.com/zedeus/nitter/wiki/Creating-session-tokens).
Without that secret, only the live suite is skipped. Secrets are not available
to pull requests from forks, so those runs use offline tests only.
