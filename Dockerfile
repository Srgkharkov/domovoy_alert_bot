# ── build ───────────────────────────────────────────────────
FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/domovoy ./cmd/domovoy \
 && mkdir -p /out/data

# ── runtime ─────────────────────────────────────────────────
# distroless/static: без shell, с CA-сертификатами и tzdata, ~2 MB
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/domovoy /domovoy
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data
USER nonroot:nonroot
ENTRYPOINT ["/domovoy"]
