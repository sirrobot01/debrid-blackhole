# Installation

There are multiple ways to install and run DecyphArr. Choose the method that works best for your setup.

## Docker Installation (Recommended)

Docker is the easiest way to get started with DecyphArr.

### Available Docker Registries

You can use either Docker Hub or GitHub Container Registry to pull the image:

- Docker Hub: `cy01/blackhole:latest`
- GitHub Container Registry: `ghcr.io/sirrobot01/decypharr:latest`

### Docker Tags

- `latest`: The latest stable release
- `beta`: The latest beta release
- `vX.Y.Z`: A specific version (e.g., `v0.1.0`)
- `nightly`: The latest nightly build (usually unstable)
- `experimental`: The latest experimental build (highly unstable)

### Docker Compose Setup

Create a `docker-compose.yml` file with the following content:

```yaml
version: '3.7'
services:
  decypharr:
    image: cy01/blackhole:latest # or cy01/blackhole:beta
    container_name: decypharr
    ports:
      - "8282:8282" # qBittorrent
      - "8181:8181" # Proxy
    user: "1000:1000"
    volumes:
      - /mnt/:/mnt
      - ./configs/:/app # config.json must be in this directory
    environment:
      - PUID=1000
      - PGID=1000
      - UMASK=002
      - QBIT_PORT=8282 # qBittorrent Port (optional)
      - PORT=8181 # Proxy Port (optional)
    restart: unless-stopped
    depends_on:
      - rclone # If you are using rclone with docker


```

Run the Docker Compose setup:
```bash
docker-compose up -d
```


## Binary Installation
If you prefer not to use Docker, you can download and run the binary directly.

Download the binary from the releases page
Create a configuration file (see Configuration)
Run the binary:
```bash
chmod +x decypharr
./decypharr --config /path/to/config
```

The config directory should contain your config.json file.