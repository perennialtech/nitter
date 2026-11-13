# Nitter

A free and open source alternative Twitter front-end focused on privacy and
performance. \
Inspired by the [Invidious](https://github.com/iv-org/invidious) project.

[![Build and publish OCI images](https://github.com/perennialtech/nitter/actions/workflows/build-docker.yml/badge.svg)](https://github.com/perennialtech/nitter/actions/workflows/build-docker.yml)
[![License](https://img.shields.io/github/license/perennialtech/nitter?style=flat)](LICENSE)

## Features

This fork includes the Catppuccin Latte, Frappé, Macchiato, and Mocha themes,
available under **Preferences → Theme**. Prebuilt images are published to
[GitHub Container Registry](https://github.com/perennialtech/nitter/pkgs/container/nitter).

- No JavaScript required
- Zero ads
- All requests go through the backend, client never talks to Twitter
- Prevents Twitter from tracking your IP or JavaScript fingerprint
- Uses Twitter's unofficial API (no developer account required)
- Lightweight
- RSS feeds (instance-specific, often disabled due to abuse)
- Themes
- Mobile support (responsive design)
- AGPLv3 licensed, no proprietary instances permitted

## Resources

The wiki contains
[a list of instances](https://github.com/zedeus/nitter/wiki/Instances) and
[browser extensions](https://github.com/zedeus/nitter/wiki/Extensions)
maintained by the community.

## Why?

It's impossible to use Twitter without JavaScript enabled, and as of 2024 you
need to sign up. For privacy-minded folks, preventing JavaScript analytics and
IP-based tracking is important, but apart from using a VPN and uBlock/uMatrix,
it's impossible. Despite being behind a VPN and using heavy-duty adblockers,
you can get accurately tracked with your [browser's
fingerprint](https://restoreprivacy.com/browser-fingerprinting/), [no
JavaScript required](https://noscriptfingerprint.com/). This all became
particularly important after Twitter [removed the
ability](https://www.eff.org/deeplinks/2020/04/twitter-removes-privacy-option-and-shows-why-we-need-strong-privacy-laws)
for users to control whether their data gets sent to advertisers.

Using an instance of Nitter (hosted on a VPS for example), you can browse
Twitter without JavaScript while retaining your privacy. In addition to
respecting your privacy, Nitter is on average around 15 times lighter than
Twitter, and in most cases serves pages faster (eg. timelines load 2-4x faster).

## Screenshot

![nitter](/screenshot.png)

## Installation

### Dependencies

- Go 1.26 or newer, see [go.dev/doc/install](https://go.dev/doc/install)
- sassc, to compile the stylesheets (`sassc` on most distros)
- make
- redis/valkey

Redis is required for caching and in the future for account info. As of 2024
Redis is no longer open source, so using the fork Valkey is recommended. It
should be available on most distros as `redis` or `redis-server`
(Ubuntu/Debian), or `valkey`/`valkey-server`. Running it with the default
config is fine, Nitter's default config is set to use the default port and
localhost.

Here's how to create a `nitter` user, clone the repo, and build the binary
and stylesheets.

```bash
# useradd -m nitter
# su nitter
$ git clone https://github.com/perennialtech/nitter
$ cd nitter
$ make build
$ cp nitter.example.conf nitter.conf
```

Set your hostname, port, HMAC key, https (must be correct for cookies), and
Redis info in `nitter.conf`. To run Redis, either run
`redis-server --daemonize yes`, or `systemctl enable --now redis` (or
redis-server depending on the distro). Run Nitter by executing `./nitter` or
using the systemd service below. You should run Nitter behind a reverse proxy
such as [Nginx](https://github.com/zedeus/nitter/wiki/Nginx) or
[Apache](https://github.com/zedeus/nitter/wiki/Apache) for security and
performance reasons.

### Docker

Images are published to `ghcr.io/perennialtech/nitter` after successful CI on
`master`. The `latest` and `master` tags, and full commit-SHA tags, support both
`linux/amd64` and `linux/arm64`. Pull request builds do not publish images.

Nitter fetches profiles, RSS timelines, media tabs, posts and their replies,
search, followers and reposters from the public
[FxTwitter API](https://docs.fxembed.com/api/) first. Everything else, and any FxTwitter request that fails, uses account
sessions from `sessions.jsonl`; without that file only the FxTwitter-backed
pages work. To create it, follow the upstream
[session token guide](https://github.com/zedeus/nitter/wiki/Creating-session-tokens).
For guest mode, see `guestAuth` in [nitter.example.conf](nitter.example.conf).

To run Nitter with Docker, you'll need to install and run Redis separately
before you can run the container. See below for how to also run Redis using
Docker.

First create your config file. The Docker commands mount it into the container,
so it has to exist on the host beforehand. If you've cloned the repo:

```bash
cp nitter.example.conf nitter.conf
```

If you're using the prebuilt image without a local clone, download
[`nitter.example.conf`](https://raw.githubusercontent.com/perennialtech/nitter/master/nitter.example.conf)
and save it as `nitter.conf` instead.

To build and run Nitter in Docker:

```bash
docker build -t nitter:latest .
docker run -v "$(pwd)/nitter.conf:/src/nitter.conf:ro" \
  -v "$(pwd)/sessions.jsonl:/src/sessions.jsonl:ro" -d --network host nitter:latest
```

A prebuilt Docker image is provided as well:

```bash
docker run -v "$(pwd)/nitter.conf:/src/nitter.conf:ro" \
  -v "$(pwd)/sessions.jsonl:/src/sessions.jsonl:ro" \
  -d --network host ghcr.io/perennialtech/nitter:latest
```

Use `compose.yml` to run both Nitter and Redis as different containers:
Change `redisHost` from `localhost` to `nitter-redis` in `nitter.conf`, then run:

```bash
docker compose up -d
```

The Docker and Compose commands mount `nitter.conf` and `sessions.jsonl`
from the directory you run them in. If a mounted file doesn't
exist, Docker silently creates a directory in its place and the container fails
with `not a directory: Are you trying to mount a directory onto a file`. Remove
that directory and create the file as shown above.

### systemd

To run Nitter via systemd you can use this service file:

```ini
[Unit]
Description=Nitter (An alternative Twitter front-end)
After=syslog.target
After=network.target

[Service]
Type=simple

# set user and group
User=nitter
Group=nitter

# configure location
WorkingDirectory=/home/nitter/nitter
ExecStart=/home/nitter/nitter/nitter

Restart=always
RestartSec=15

[Install]
WantedBy=multi-user.target
```

Then enable and run the service:
`systemctl enable --now nitter.service`

### Logging

Nitter currently prints some errors to stdout, and there is no real logging
implemented. If you're running Nitter with systemd, you can check stdout like
this: `journalctl -u nitter.service` (add `--follow` to see just the last 15
lines). If you're running the Docker image, you can do this:
`docker logs --follow *nitter container id*`

## Contributing

For build and test instructions, see [AGENTS.md](AGENTS.md).
