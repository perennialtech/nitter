.PHONY: build css generate check

build: css
	go build -trimpath -o nitter ./cmd/nitter

css:
	sassc -I sass/include sass/index.scss public/css/style.css

# Writes nitter.example.conf and nitter.example.env from the option
# definitions; check fails until they are current.
generate:
	go run ./cmd/genconfig

# The gates CI runs.
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
