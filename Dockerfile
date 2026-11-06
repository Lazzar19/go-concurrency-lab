# syntax=docker/dockerfile:1

# CPU timings follow GOMAXPROCS. This image does not set it.
FROM golang:1.27.1-bookworm AS build

WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.22 AS runtime

RUN adduser -D -H -u 65532 app
COPY --from=build --chown=65532:65532 /out/server /server
USER 65532

ENV PORT=8080
# Opt in to every interface. A local `go run` leaves HOST empty and stays on 127.0.0.1.
ENV HOST=0.0.0.0
EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/health" || exit 1

ENTRYPOINT ["/server"]
