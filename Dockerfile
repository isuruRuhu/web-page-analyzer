FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

# The distroless static image has CA certificates, which the analyzer needs
# for HTTPS pages, and no shell. It runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /server /server

# Inside a container localhost is not reachable from the host, so the debug
# server listens on all interfaces. Publish 6060 only where it is safe.
ENV ADDR=:9090 \
    DEBUG_ADDR=:6060
EXPOSE 9090 6060

ENTRYPOINT ["/server"]
