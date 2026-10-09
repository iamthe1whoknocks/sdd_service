# VulnRAG API — многоступенчатая сборка Go-приложения
#
# Stage 1: сборка статического бинаря (CGO выключен).
# Stage 2: минимальный runtime на базе alpine + curl для healthcheck.

FROM golang:1.23-alpine AS builder

WORKDIR /src

# Кэш зависимостей: сначала go.mod/go.sum, затем исходники
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

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/server"]