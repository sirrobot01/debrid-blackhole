### GoBlackHole(with Debrid Proxy Support)

This is a Golang implementation go Torrent Blackhole with a **Real Debrid Proxy Support**.

#### Uses
- Torrent Blackhole that supports the Arrs(Sonarr, Radarr, etc)
- Proxy support for the Arrs

The proxy is useful in filtering out un-cached Real Debrid torrents

### Changelog

- View the [CHANGELOG.md](CHANGELOG.md) for the latest changes


#### Installation
##### Docker Compose
```yaml
version: '3.7'
services:
  blackhole:
    image: cy01/blackhole:latest # or cy01/blackhole:beta
    container_name: blackhole
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
    "password": "password",
    "cached_only": true
  },
  "max_cache_size": 1000
}
```

#### Config Notes
##### Debrid Config
- This config key is important as it's used for both Blackhole and Proxy

##### Arrs Config
- An empty array will disable Blackhole for the Arrs
- The `watch_folder` is the folder where the Blackhole will watch for torrents
- The `completed_folder` is the folder where the Blackhole will move the completed torrents
- The `token` is the API key for the Arr(This is optional, I think)

##### Proxy Config
- The `enabled` key is used to enable the proxy
- The `port` key is the port the proxy will listen on
- The `debug` key is used to enable debug logs
- The `username` and `password` keys are used for basic authentication
- The `cached_only` means only cached torrents will be returned
- 
### Proxy

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