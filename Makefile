BINARY := bin/server
IMAGE  := web-page-analyzer

.PHONY: build run test cover fmt vet clean docker-build docker-run

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

docker-build:
	docker build -t $(IMAGE) .

# The debug port is published on localhost only.
docker-run: docker-build
	docker run --rm -p 9090:9090 -p 127.0.0.1:6060:6060 $(IMAGE)
