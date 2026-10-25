.PHONY: run build test check
run:
	go run ./cmd/server
build:
	go build -o server ./cmd/server
test:
	go test -race -count=1 ./...
check: test
	go vet ./...
	node --test static/js/palette.test.cjs
