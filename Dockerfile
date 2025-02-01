FROM --platform=$BUILDPLATFORM golang:1.22 as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG VERSION
ARG CHANNEL

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

ADD . .

RUN CGO_ENABLED=0 GOOS=$(echo $TARGETPLATFORM | cut -d '/' -f1) GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) go build \
    -ldflags="-X github.com/sirrobot01/debrid-blackhole/pkg/version.Version=${VERSION} -X github.com/sirrobot01/debrid-blackhole/pkg/version.Channel=${CHANNEL}" \
    -o /blackhole

RUN CGO_ENABLED=0 GOOS=$(echo $TARGETPLATFORM | cut -d '/' -f1) GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) go build \
    -o /healthcheck cmd/healthcheck/main.go

FROM alpine as logsetup
ARG PUID=1000
ARG PGID=1000
RUN addgroup -g $PGID appuser && \
    adduser -D -u $PUID -G appuser appuser && \
    mkdir -p /logs && \
    chmod 777 /logs && \
    touch /logs/decypharr.log && \
    chmod 666 /logs/decypharr.log

FROM gcr.io/distroless/static-debian12:latest
COPY --from=builder /blackhole /blackhole
COPY --from=builder /healthcheck /healthcheck
COPY --from=logsetup /etc/passwd /etc/passwd
COPY --from=logsetup /etc/group /etc/group
COPY --from=logsetup /logs /logs

ENV LOG_PATH=/logs

EXPOSE 8181 8282

VOLUME ["/app"]

USER appuser

HEALTHCHECK CMD ["/healthcheck"]

CMD ["/blackhole", "--config", "/app/config.json"]