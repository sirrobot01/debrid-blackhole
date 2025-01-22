### DecyphArr(with Debrid Proxy Support)

![ui](doc/main.png)

This is a Golang implementation go Torrent QbitTorrent with a **Multiple Debrid service support**.

### Table of Contents

- [Features](#features)
- [Supported Debrid Providers](#supported-debrid-providers)
- [Installation](#installation)
  - [Docker Compose](#docker-compose)
  - [Binary](#binary)
- [Usage](#usage)
- [Connecting to Sonarr/Radarr](#connecting-to-sonarrradarr)
- [Sample Config](#sample-config)
- [Config Notes](#config-notes)
  - [Log Level](#log-level)
  - [Max Cache Size](#max-cache-size)
  - [Debrid Config](#debrid-config)
  - [Proxy Config](#proxy-config)
  - [Qbittorrent Config](#qbittorrent-config)
  - [Arrs Config](#arrs-config)
- [Proxy](#proxy)
- [Repair Worker](#repair-worker)
- [Changelog](#changelog)
- [TODO](#todo)

### Features

- Mock Qbittorent API that supports the Arrs(Sonarr, Radarr, etc)
- A Full-fledged UI for managing torrents
- Proxy support for the Arrs
- Real Debrid Support
- Torbox Support
- Debrid Link Support
- Multi-Debrid Providers support
- Repair Worker for missing files (**NEW**)

The proxy is useful in filtering out un-cached Real Debrid torrents

### Supported Debrid Providers
- [Real Debrid](https://real-debrid.com)
- [Torbox](https://torbox.app)
- [Debrid Link](https://debrid-link.com)
- [All Debrid](https://alldebrid.com)


### Installation

##### Docker Compose
```yaml
version: '3.7'
services:
  blackhole:
    image: cy01/blackhole:latest # or cy01/blackhole:beta
    container_name: blackhole
    ports:
      - "8282:8282" # qBittorrent
      - "8181:8181" # Proxy
    user: "1000:1000"
    volumes:
      - ./logs/:/app/logs
      - /mnt/:/mnt
      - ~/plex/configs/blackhole/config.json:/app/config.json # Config file, see below
    environment:
      - PUID=1000
      - PGID=1000
      - UMASK=002
      - QBIT_PORT=8282 # qBittorrent Port. This is optional. You can set this in the config file
      - PORT=8181 # Proxy Port. This is optional. You can set this in the config file
    restart: unless-stopped
    depends_on:
      - rclone # If you are using rclone with docker
    
```

##### Binary
Download the binary from the releases page and run it with the config file.

```bash
./blackhole --config /path/to/config.json
```

### Usage
- The UI is available at `http://localhost:8282`
- Setup the config.json file. Scroll down for the sample config file
- Setup docker compose/ binary with the config file
- Start the service
- Connect to Sonarr/Radarr/Lidarr

#### Connecting to Sonarr/Radarr

- Sonarr/Radarr
  - Settings -> Download Client -> Add Client -> qBittorrent
  - Host: `localhost` # or the IP of the server
  - Port: `8282` # or the port set in the config file/ docker-compose env
  - Username: `http://sonarr:8989` # Your arr host with http/https
  - Password: `sonarr_token` # Your arr token
  - Category: e.g `sonarr`, `radarr`
  - Use SSL -> `No`
  - Sequential Download -> `No`|`Yes` (If you want to download the torrents locally instead of symlink)
  - Test
  - Save

#### Sample Config

This is the default config file. You can create a `config.json` file in the root directory of the project or mount it in the docker-compose file.
```json
{
  "debrids": [
    {
      "name": "torbox",
      "host": "https://api.torbox.app/v1",
      "api_key": "torbox_api_key",
      "folder": "/mnt/remote/torbox/torrents/",
      "rate_limit": "250/minute",
      "download_uncached": false,
      "check_cached": true
    },
    {
      "name": "realdebrid",
      "host": "https://api.real-debrid.com/rest/1.0",
      "api_key": "realdebrid_key",
      "folder": "/mnt/remote/realdebrid/__all__/",
      "rate_limit": "250/minute",
      "download_uncached": false,
      "check_cached": false
    },
    {
      "name": "debridlink",
      "host": "https://debrid-link.com/api/v2",
      "api_key": "debridlink_key",
      "folder": "/mnt/remote/debridlink/torrents/",
      "rate_limit": "250/minute",
      "download_uncached": false,
      "check_cached": false
    },
    {
      "name": "alldebrid",
      "host": "http://api.alldebrid.com/v4.1",
      "api_key": "alldebrid_key",
      "folder": "/mnt/remote/alldebrid/magnet/",
      "rate_limit": "600/minute",
      "download_uncached": false,
      "check_cached": false
    }
  ],
  "proxy": {
    "enabled": true,
    "port": "8100",
    "log_level": "info",
    "username": "username",
    "password": "password",
    "cached_only": true
  },
  "max_cache_size": 1000,
  "qbittorrent": {
    "port": "8282",
    "download_folder": "/mnt/symlinks/",
    "categories": ["sonarr", "radarr"],
    "refresh_interval": 5,
    "log_level": "info"
  },
  "arrs": [
    {
      "name": "sonarr",
      "host": "http://host:8989",
      "token": "arr_key"
    },
    {
      "name": "radarr",
      "host": "http://host:7878",
      "token": "arr_key"
    }
  ],
  "repair": {
    "enabled": true,
    "interval": "12h",
    "run_on_start": false
  },
  "log_level": "info"
}
```

#### Config Notes

##### Log Level
- The `log_level` key is used to set the log level of the application. The default value is `info`
- The log level can be set to `debug`, `info`, `warn`, `error`
##### Max Cache Size
- The `max_cache_size` key is used to set the maximum number of infohashes that can be stored in the availability cache. This is used to prevent round trip to the debrid provider when using the proxy/Qbittorrent
- The default value is `1000`
- The cache is stored in memory and is not persisted on restart

##### Debrid Config
- The `debrids` key is an array of debrid providers
- The `name` key is the name of the debrid provider
- The `host` key is the API endpoint of the debrid provider
- The `api_key` key is the API key of the debrid provider
- The `folder` key is the folder where your debrid folder is mounted(webdav, rclone, zurg etc). e.g `data/realdebrid/torrents/`, `/media/remote/alldebrid/magnets/`
- The `rate_limit` key is the rate limit of the debrid provider(null by default)
- The `download_uncached` bool key is used to download uncached torrents(disabled by default)
- The `check_cached` bool key is used to check if the torrent is cached(disabled by default)

##### Repair Config (**NEW**)
The `repair` key is used to enable the repair worker
- The `enabled` key is used to enable the repair worker
- The `interval` key is the interval in either minutes, seconds, hours, days. Use any of this format, e.g 12:00, 5:00, 1h, 1d, 1m, 1s.
- The `run_on_start` key is used to run the repair worker on start

##### Proxy Config
- The `enabled` key is used to enable the proxy
- The `port` key is the port the proxy will listen on
- The `log_level` key is used to set the log level of the proxy. The default value is `info`
- The `username` and `password` keys are used for basic authentication
- The `cached_only` means only cached torrents will be returned


##### Qbittorrent Config
- The `port` key is the port the qBittorrent will listen on
- The `download_folder` is the folder where the torrents will be downloaded. e.g `/media/symlinks/`
- The `categories` key is used to filter out torrents based on the category. e.g `sonarr`, `radarr`
- The `refresh_interval` key is used to set the interval in minutes to refresh the Arrs Monitored Downloads(it's in seconds). The default value is `5` seconds


##### Arrs Config
This is an array of Arrs(Sonarr, Radarr, etc) that will be used to download the torrents. This is not required if you already set up the Qbittorrent in the Arrs with the host, token.
This is particularly useful if you want to use the Repair tool without using Qbittorent
- The `name` key is the name of the Arr/ Category
- The `host` key is the host of the Arr
- The `token` key is the API token of the Arr


### Proxy

The proxy is useful in filtering out un-cached Real Debrid torrents. 
The proxy is a simple HTTP proxy that requires basic authentication. The proxy can be enabled by setting the `proxy.enabled` to `true` in the config file. 
The proxy listens on the port `8181` by default. The username and password can be set in the config file.

### Repair Worker

The repair worker is a simple worker that checks for missing files in the Arrs(Sonarr, Radarr, etc). It's particularly useful for files either deleted by the Debrid provider or files with bad symlinks.

- Search for broken symlinks/files
- Search for missing files
- Search for deleted/unreadable files


### Changelog

- View the [CHANGELOG.md](CHANGELOG.md) for the latest changes


### TODO
- [ ] A proper name!!!!
- [x] Debrid
  - [x] Add more Debrid Providers

- [ ] Qbittorrent
  - [ ] Add more Qbittorrent features
  - [x] Persist torrents on restart/server crash
- [ ] Add tests