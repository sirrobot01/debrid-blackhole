FROM --platform=$BUILDPLATFORM golang:1.22 as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
ADD . .

# Build
RUN CGO_ENABLED=0 GOOS=$(echo $TARGETPLATFORM | cut -d '/' -f1) GOARCH=$(echo $TARGETPLATFORM | cut -d '/' -f2) go build -o /blackhole

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /blackhole /blackhole
COPY --from=builder /app/README.md /README.md

EXPOSE 8181

# Run
CMD ["/blackhole", "--config", "/app/config.json"]