# Stage 1: Build binaries
FROM --platform=$BUILDPLATFORM golang:1.23-alpine as builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.0
ARG CHANNEL=dev

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download -x

COPY . .

# Build main binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
    -ldflags="-w -s -X github.com/myhme/decypharr/pkg/version.Version=${VERSION} -X github.com/myhme/decypharr/pkg/version.Channel=${CHANNEL}" \
    -o /blackhole

# Build healthcheck (optimized)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-w -s" \
    -o /healthcheck cmd/healthcheck/main.go

# Stage 2: Create directory structure
FROM alpine:3.19 as dirsetup
RUN mkdir -p /app/logs && \
    chmod 777 /app/logs && \
    touch /app/logs/decypharr.log && \
    chmod 666 /app/logs/decypharr.log

# Stage 3: Final image
FROM gcr.io/distroless/static-debian12:nonroot

LABEL version = "${VERSION}-${CHANNEL}"

LABEL org.opencontainers.image.source = "https://github.com/sirrobot01/debrid-blackhole"
LABEL org.opencontainers.image.title = "debrid-blackhole"
LABEL org.opencontainers.image.authors = "sirrobot01"
LABEL org.opencontainers.image.documentation = "https://github.com/sirrobot01/debrid-blackhole/blob/main/README.md"

# Copy binaries
COPY --from=builder --chown=nonroot:nonroot /blackhole /usr/bin/blackhole
COPY --from=builder --chown=nonroot:nonroot /healthcheck /usr/bin/healthcheck

# Copy pre-made directory structure
COPY --from=dirsetup --chown=nonroot:nonroot /app /app


# Metadata
ENV LOG_PATH=/app/logs
EXPOSE 8181 8282
VOLUME ["/app"]
USER nonroot:nonroot

HEALTHCHECK CMD ["/usr/bin/healthcheck"]

CMD ["/usr/bin/blackhole", "--config", "/app"]