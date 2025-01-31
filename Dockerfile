FROM --platform=$BUILDPLATFORM golang:1.22 as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG VERSION
ARG CHANNEL

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
ADD . .

# Build
RUN CGO_ENABLED=0 GOOS=$(echo $TARGETPLATFORM | cut -d '/' -f1) GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) go build -ldflags="-X github.com/sirrobot01/debrid-blackhole/pkg/version.Version=${VERSION} -X github.com/sirrobot01/debrid-blackhole/pkg/version.Channel=${CHANNEL}" -o /blackhole

RUN CGO_ENABLED=0 GOOS=$(echo $TARGETPLATFORM | cut -d '/' -f1) GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) go build -o /healthcheck cmd/healthcheck/main.go

FROM alpine as logsetup
ARG PUID=1000
ARG PGID=1000
RUN addgroup -g $PGID appuser && \
    adduser -D -u $PUID -G appuser appuser && \
    mkdir -p /logs && \
    chown -R appuser:appuser /logs && \
    mkdir -p /app && \
    chown appuser:appuser /app

FROM gcr.io/distroless/static-debian12:latest
COPY --from=builder /blackhole /blackhole
COPY --from=builder /healthcheck /healthcheck
COPY --from=builder /app/README.md /README.md
COPY --from=logsetup /etc/passwd /etc/passwd
COPY --from=logsetup /etc/group /etc/group
COPY --from=logsetup /logs /logs
COPY --from=logsetup /app /app


ENV LOG_PATH=/logs

EXPOSE 8181 8282

VOLUME ["/app"]

USER appuser

HEALTHCHECK CMD ["/healthcheck"]

# Run
CMD ["/blackhole", "--config", "/app/config.json"]
