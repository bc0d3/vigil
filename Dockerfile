# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
FROM golang:1.25-alpine AS build

# ca-certificates para copiarlas a la imagen final (scratch no trae ninguna).
RUN apk add --no-cache ca-certificates

WORKDIR /src

# Cachear módulos: copiar go.mod/go.sum y descargar antes del código.
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

ARG VERSION=docker
ARG COMMIT=none
ARG DATE=unknown

# Binario estático: sin CGO -> corre en scratch.
RUN CGO_ENABLED=0 GOFLAGS=-trimpath \
    go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /vigil ./cmd/vigil

# --- final stage -------------------------------------------------------------
FROM scratch

LABEL org.opencontainers.image.title="vigil" \
      org.opencontainers.image.description="Content fingerprinting & change detection CLI" \
      org.opencontainers.image.source="https://github.com/bc0d3/vigil" \
      org.opencontainers.image.licenses="MIT"

# Certificados raíz para validar TLS al salir a la red.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /vigil /vigil

# Correr como no-root (uid:gid nobody). `scan` no escribe a disco, así que la
# imagen funciona con --read-only y sin capabilities. 65534 es numérico a
# propósito: scratch no tiene /etc/passwd para resolver un nombre.
USER 65534:65534

# Invocar con: docker run --rm <image> scan <url>
ENTRYPOINT ["/vigil"]
