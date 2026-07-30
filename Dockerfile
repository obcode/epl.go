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

# Auf die Patch-Version gepinnt, nicht `latest` und nicht `3.24`: nur an einem
# vergleichbaren Tag kann Dependabot ein Update erkennen. Der PR wird zu einem
# fix(docker)-Commit, der einen Patch-Release erzeugt — und erst der baut das Image neu
# und rollt es aus. Mit einem gleitenden Tag bliebe ein Base-Image-CVE ungefixt, weil
# ohne Release nie neu gebaut wird.
FROM alpine:3.24.1

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
