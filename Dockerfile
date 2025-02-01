# Stage 1: Build binaries
FROM --platform=$BUILDPLATFORM golang:1.22-alpine as builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG CHANNEL

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
    -ldflags="-w -s -X github.com/sirrobot01/debrid-blackhole/pkg/version.Version=${VERSION} -X github.com/sirrobot01/debrid-blackhole/pkg/version.Channel=${CHANNEL}" \
    -o /blackhole

# Build healthcheck (optimized)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-w -s" \
    -o /healthcheck cmd/healthcheck/main.go

# Stage 2: Create directory structure
FROM alpine:3.19 as dirsetup
RUN mkdir -p /logs && \
    chmod 777 /logs && \
    touch /logs/decypharr.log && \
    chmod 666 /logs/decypharr.log

# Stage 3: Final image
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binaries
COPY --from=builder --chown=nonroot:nonroot /blackhole /blackhole
COPY --from=builder --chown=nonroot:nonroot /healthcheck /healthcheck

# Copy pre-made directory structure
COPY --from=dirsetup --chown=nonroot:nonroot /logs /logs

# Metadata
ENV LOG_PATH=/logs
EXPOSE 8181 8282
VOLUME ["/app"]
USER nonroot:nonroot
HEALTHCHECK CMD ["/healthcheck"]
CMD ["/blackhole", "--config", "/app/config.json"]