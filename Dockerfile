# syntax=docker/dockerfile:1.7
#
# Multi-stage build for the RFSPager Go API server.
# Final image is distroless, ~25 MB, runs as non-root.

FROM golang:1.27.4-alpine AS build
WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Build
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
        -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
COPY LICENSE.md NOTICE THIRD_PARTY_NOTICES.md /
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
