# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder

ARG VERSION=dev
ARG GIT_COMMIT=none
ARG BUILD_TIME=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${GIT_COMMIT} -X main.date=${BUILD_TIME}" \
    -o /out/epl .

FROM alpine:latest

# tzdata ist Pflicht, nicht Komfort: main.go setzt time.Local auf Europe/Berlin, und
# Meilenstein-Fristen sowie Phasenwechsel hängen daran. Ohne tzdata fiele der Prozess
# stillschweigend auf UTC zurück.
RUN apk add --no-cache ca-certificates tzdata

# Nicht als root. Feste UID, damit ein gemountetes Volume vorhersagbare Besitzverhältnisse hat.
RUN adduser -D -u 10001 epl

WORKDIR /app
COPY --from=builder /out/epl /app/epl

USER epl
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/epl"]
