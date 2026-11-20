# Builds the hody binary into a small non-root image.
# Note: the web page listens on 127.0.0.1 inside the container, so for the
# web page run the binary directly on your machine. The image is meant for
# `hody diagnose` and `hody eval`.
#
#   docker build -f deploy/Dockerfile -t hody .
#   docker run --rm -v /var/run/docker.sock:/var/run/docker.sock:ro \
#     -e HODY_LLM_BASE_URL -e HODY_LLM_API_KEY -e HODY_LLM_MODEL \
#     hody diagnose docker <container>
# Mounting the socket read-only does not make the Docker API read-only;
# Hody's own client is what restricts itself to GET requests.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hody ./cmd/hody

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hody /usr/local/bin/hody
ENV HODY_DATA_DIR=/tmp/hody
ENTRYPOINT ["/usr/local/bin/hody"]
