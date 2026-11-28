FROM golang:1.25-bookworm AS builder

WORKDIR /src

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod ./
RUN go mod download || true

COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aegis-server ./cmd/server

FROM debian:bookworm-slim AS runner

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    poppler-utils \
    tesseract-ocr \
    tesseract-ocr-eng \
    tesseract-ocr-chi-sim \
    tesseract-ocr-chi-tra \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 appgroup \
    && useradd --uid 10001 --gid appgroup --shell /usr/sbin/nologin --no-create-home appuser

WORKDIR /app
COPY --from=builder /out/aegis-server /app/aegis-server

RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 8080
EXPOSE 8443/udp
EXPOSE 8443/tcp

ENTRYPOINT ["/app/aegis-server"]
