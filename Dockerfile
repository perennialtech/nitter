FROM golang:1.27-alpine AS build

# git lets go build stamp the commit shown on the about page.
RUN apk add --no-cache git make sassc

WORKDIR /src/nitter
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make check && CGO_ENABLED=0 make build

FROM debian:bookworm-slim
WORKDIR /src/
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/nitter/nitter ./
COPY --from=build /src/nitter/nitter.example.conf ./nitter.conf
COPY --from=build /src/nitter/public ./public
EXPOSE 8080
RUN useradd --home-dir /src --no-create-home --shell /usr/sbin/nologin nitter
USER nitter
CMD ["./nitter"]
