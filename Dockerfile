FROM golang:1.27-alpine AS build

# git lets go build stamp the commit shown on the about page.
RUN apk add --no-cache ca-certificates git make sassc

WORKDIR /src/nitter
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make check && CGO_ENABLED=0 make build

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /src/
COPY --from=build /src/nitter/nitter ./
COPY --from=build /src/nitter/nitter.example.conf ./nitter.conf
COPY --from=build /src/nitter/public ./public
EXPOSE 8080
USER 998:998
CMD ["./nitter"]
