# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.3

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/triagearr ./cmd/triagearr

FROM gcr.io/distroless/static-debian13:nonroot

LABEL org.opencontainers.image.source="https://github.com/Triagearr/Triagearr"
LABEL org.opencontainers.image.description="Disk-pressure-aware media reaper for Plex/*arr/qBittorrent"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=builder /out/triagearr /usr/local/bin/triagearr

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/triagearr"]
CMD ["serve"]
