.PHONY: build release test check

build:
	mkdir -p build
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/mysql-monitor-connector ./cmd/mysql-monitor-connector

release:
	mkdir -p build
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/mysql-monitor-connector-linux-amd64 ./cmd/mysql-monitor-connector
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/mysql-monitor-connector-linux-arm64 ./cmd/mysql-monitor-connector

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...
