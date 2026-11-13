.PHONY: build css check

build: css
	go build -trimpath -o nitter ./cmd/nitter

css:
	sassc -I src/sass/include src/sass/index.scss public/css/style.css

# The gates CI runs.
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
