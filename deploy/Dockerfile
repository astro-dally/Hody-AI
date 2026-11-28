# syntax=docker/dockerfile:1
# Builds the hody binary into a small non-root image, for linux/amd64 and
# linux/arm64 (the Go compiler cross-compiles, so no emulation is needed).
#
#   docker build -f deploy/Dockerfile -t hody .
#   docker run --rm hody version
#
# Diagnosing Docker containers from inside a container needs the Docker
# socket. Mounting it, even with :ro, gives full control of Docker: :ro
# makes the socket file read-only, not the Docker API. Hody's own client
# only sends GET requests to an allowlist of paths (enforced and tested),
# but the container could do anything the socket allows, so on your own
# machine prefer the binary (README.md, "Install"). The web page listens on
# 127.0.0.1 inside the container, so for it run the binary directly too.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev COMMIT=none DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/hody ./cmd/hody

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hody /usr/local/bin/hody
ENV HODY_DATA_DIR=/tmp/hody
ENTRYPOINT ["/usr/local/bin/hody"]
