### GoBlackHole(with Debrid Proxy Support)

This is a Golang implementation go Torrent Blackhole with a **Real Debrid Proxy Support**.

#### Uses
- Torrent Blackhole that supports the Arrs.
- Proxy support for the Arrs

The proxy is useful in filtering out un-cached Real Debrid torrents


#### Installation
##### Docker Compose
```yaml
version: '3.7'
services:
  blackhole:
    image: ghcr.io/sirrobot01/debrid-blackhole:latest
    container_name: debrid-blackhole
    user: "1000:1000"
    volumes:
      - ./logs:/app/logs
      - ~/plex/media:/media
      - ~/plex/media/symlinks/:/media/symlinks/
      - ~/plex/configs/blackhole/config.json:/app/config.json # Config file, see below
    environment:
      - PUID=1000
      - PGID=1000
      - UMASK=002
    restart: unless-stopped
    
```

##### Binary
Download the binary from the releases page and run it with the config file.

```bash
./blackhole --config /path/to/config.json
```

#### Config
```json
{
  "debrid": {
    "name": "realdebrid",
    "host": "https://api.real-debrid.com/rest/1.0",
    "api_key": "realdebrid_api_key",
    "folder": "data/realdebrid/torrents/",
    "rate_limit": "250/minute"
  },
  "arrs": [
    {
      "watch_folder": "data/sonarr/",
      "completed_folder": "data/sonarr/completed/",
      "token": "sonarr_api_key",
      "url": "http://localhost:8787"
    },
    {
      "watch_folder": "data/radarr/",
      "completed_folder": "data/radarr/completed/",
      "token": "radarr_api_key",
      "url": "http://localhost:7878"
    },
    {
      "watch_folder": "data/radarr4k/",
      "completed_folder": "data/radarr4k/completed/",
      "token": "radarr4k_api_key",
      "url": "http://localhost:7878"
    }
  ],
  "proxy": {
    "enabled": true,
    "port": "8181",
    "debug": false,
    "username": "username",
    "password": "password"
  }
}
```

#### Proxy

The proxy is useful in filtering out un-cached Real Debrid torrents. 
The proxy is a simple HTTP proxy that requires basic authentication. The proxy can be enabled by setting the `proxy.enabled` to `true` in the config file. 
The proxy listens on the port `8181` by default. The username and password can be set in the config file.

Setting Up Proxy in Arr

- Sonarr/Radarr
  - Settings -> General -> Use Proxy
  - Hostname: `localhost` # or the IP of the server
  - Port: `8181` # or the port set in the config file
  - Username: `username` # or the username set in the config file
  - Password: `password` # or the password set in the config file
  - Bypass Proxy for Local Addresses -> `No`

### TODO
- [ ] Add more Debrid Providers
- [ ] Add more Proxy features
- [ ] Add more tests