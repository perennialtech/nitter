FROM nim:2.2.12 AS nim
LABEL maintainer="setenforce@protonmail.com"

RUN apt-get update \
    && apt-get install -y --no-install-recommends libsass-dev python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/nitter

COPY nitter.nimble .
RUN nimble install -y --depsOnly

COPY . .
RUN python3 tests/test_http_proxy.py -v \
    && nim r tests/test_ssrf_1411.nim \
    && nim r tests/test_formatters.nim \
    && nim r tests/test_timeline.nim \
    && nimble build -d:danger -d:lto -d:strip --mm:refc \
    && nimble scss \
    && nimble md

FROM debian:bookworm-slim
WORKDIR /src/
RUN apt-get update \
    && apt-get install -y --no-install-recommends libpcre3 ca-certificates openssl wget \
    && rm -rf /var/lib/apt/lists/*
COPY --from=nim /src/nitter/nitter ./
COPY --from=nim /src/nitter/nitter.example.conf ./nitter.conf
COPY --from=nim /src/nitter/public ./public
EXPOSE 8080
RUN useradd --home-dir /src --no-create-home --shell /usr/sbin/nologin nitter
USER nitter
CMD ["./nitter"]
