# syntax=docker/dockerfile:1.7
#
# Runtime-only image. The binary is built by goreleaser (see .goreleaser.yaml)
# and passed into the build context alongside this Dockerfile. To build a
# standalone image locally without goreleaser, use `make docker` which invokes
# `goreleaser release --snapshot --clean --skip=publish`.

FROM gcr.io/distroless/static-debian13:nonroot

LABEL org.opencontainers.image.source="https://github.com/Triagearr/Triagearr"
LABEL org.opencontainers.image.description="Disk-pressure-aware media reaper for Plex/*arr/qBittorrent"
LABEL org.opencontainers.image.licenses="MIT"

COPY triagearr /usr/local/bin/triagearr

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/triagearr"]
CMD ["serve"]
