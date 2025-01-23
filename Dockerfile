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

FROM alpine as logsetup
RUN mkdir -p /logs && \
    touch /logs/decypharr.log && \
    chown -R 1000:1000 /logs && \
    chmod -R 755 /logs && \
    chmod 666 /logs/decypharr.log

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /blackhole /blackhole
COPY --from=builder /app/README.md /README.md
COPY --from=logsetup /logs /logs

ENV LOG_PATH=/logs

EXPOSE 8181 8282

VOLUME ["/app"]

# Run
CMD ["/blackhole", "--config", "/app/config.json"]
