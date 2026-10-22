FROM nimlang/nim:2.2.6-alpine-regular AS nim
LABEL maintainer="setenforce@protonmail.com"

RUN apk --no-cache add libsass-dev pcre patch python3 openssl

# HTTPS tunnels require origin-form request targets (Nim issue #25319).
COPY tools/nim-httpclient-connect.patch /tmp/nim-httpclient-connect.patch
RUN patch --batch --forward -p1 -d /nim < /tmp/nim-httpclient-connect.patch

WORKDIR /src/nitter

COPY nitter.nimble .
RUN nimble install -y --depsOnly

COPY . .
RUN python3 tests/test_http_proxy.py -v \
    && nimble build -d:danger -d:lto -d:strip --mm:refc \
    && nimble scss \
    && nimble md

FROM alpine:latest
WORKDIR /src/
RUN apk --no-cache add pcre ca-certificates openssl
COPY --from=nim /src/nitter/nitter ./
COPY --from=nim /src/nitter/nitter.example.conf ./nitter.conf
COPY --from=nim /src/nitter/public ./public
EXPOSE 8080
RUN adduser -h /src/ -D -s /bin/sh nitter
USER nitter
CMD ./nitter
