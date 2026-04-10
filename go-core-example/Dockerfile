# Build stage
# Context must be the go-core/ root (parent of go-core-example/) so that
# the "replace github.com/wssto2/go-core => ../" directive in go.mod resolves.
FROM golang:latest AS builder

# Download the required toolchain version automatically (needed for go 1.25+)
ENV GOTOOLCHAIN=auto

WORKDIR /build

# Copy the entire go-core root (library source + example subdirectory)
COPY . .

# Build the example binary
WORKDIR /build/go-core-example
RUN CGO_ENABLED=1 go build -o /api ./cmd/api/

# Runtime stage — Debian slim keeps glibc for CGO-linked sqlite3
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /api /api

EXPOSE 8080
ENTRYPOINT ["/api"]
