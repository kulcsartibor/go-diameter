# Linux baseline runs for the gyspike throughput harness. macOS/Darwin
# throttles loopback enough to distort results (measured ~6x on raw socket
# paths), so numbers quoted for comparison should come from here.
#
# Build context = repo root:
#   docker build -f cmd/gyspike/Dockerfile -t gyspike:local .
#   docker run --rm gyspike:local gyspike --role both --conns 16 --window 128 --secs 4 --warmup 1
#
# Uses the staticodec/gy static codec over goroutine-per-connection
# networking (runtime netpoller / epoll).
FROM golang:1.25-bookworm

WORKDIR /src
COPY . /src
RUN go build -o /usr/local/bin/gyspike ./cmd/gyspike

# Fixtures ship in the repo; gyspike defaults to bench/fixtures.
WORKDIR /src
CMD ["gyspike", "--role", "both", "--conns", "16", "--window", "128", "--secs", "4", "--warmup", "1"]
