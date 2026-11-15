BINARY := bin/server

.PHONY: build run test cover fmt vet clean

build:
	go build -o $(BINARY) ./cmd/server

run: build
	./$(BINARY)

test:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin coverage.out
