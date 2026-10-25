# VulnRAG API — multi-stage build of the Go application
#
# Stage 1: build a static binary (CGO disabled).
# Stage 2: minimal runtime on alpine + curl for the compose healthcheck.
# Note: HEALTHCHECK is defined in docker-compose.yml (app service) so that
# interval/timeout can be tuned without rebuilding the image.

FROM golang:latest AS builder

WORKDIR /src

# Dependency cache: copy go.mod/go.sum first, then sources
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 2: runtime ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates curl && \
    addgroup -S app && adduser -S -G app app

COPY --from=builder /out/server /usr/local/bin/server

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/server"]