### DecyphArr(Qbittorent, but with Debrid Support)

![ui](doc/main.png)

This is an implementation of QbitTorrent with a **Multiple Debrid service support**. Written in Go.

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
  - [Log Level]()
  - [Max Cache Size](#max-cache-size)
  - [Debrid Config](#debrid-config)
  - [Proxy Config](#proxy-config)
  - [Qbittorrent Config](#qbittorrent-config)
  - [Arrs Config](#arrs-config)
- [Repair Worker](#repair-worker)
- [WebDAV](#webdav)
  - [WebDAV Config](#webdav-config)
- [Changelog](#changelog)
- [TODO](#todo)

### Features

- Mock Qbittorent API that supports the Arrs(Sonarr, Radarr, Lidarr etc)
- A Full-fledged UI for managing torrents
- Proxy support for the Arrs
- Multiple Debrid providers
- WebDAV server support for each debrid provider
- Repair Worker for missing files

The proxy is useful for filtering out un-cached Debrid torrents

### Supported Debrid Providers
- [Real Debrid](https://real-debrid.com)
- [Torbox](https://torbox.app)
- [Debrid Link](https://debrid-link.com)
- [All Debrid](https://alldebrid.com)


### Installation

##### Docker

###### Registry
You can use either hub.docker.com or ghcr.io to pull the image. The image is available on both platforms.

- Docker Hub: `cy01/blackhole:latest`
- GitHub Container Registry: `ghcr.io/sirrobot01/decypharr:latest`

###### Tags

- `latest`: The latest stable release
- `beta`: The latest beta release
- `vX.Y.Z`: A specific version (e.g `v0.1.0`)
- `nightly`: The latest nightly build. This is usually unstable
- `experimental`: The latest experimental build. This is highly unstable!!


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
      - ~/plex/configs/decypharr/:/app # config.json must be in this directory
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
./decypharr --config /app
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
  - Click Test
  - Click Save

#### Basic Sample Config

This is the default config file. You can create a `config.json` file in the root directory of the project or mount it to /app in the docker-compose file.
```json
{
  "debrids": [
    {
      "name": "realdebrid",
      "host": "https://api.real-debrid.com/rest/1.0",
      "api_key": "realdebrid_key",
      "folder": "/mnt/remote/realdebrid/__all__/"
    }
  ],
  "qbittorrent": {
    "port": "8282",
    "download_folder": "/mnt/symlinks/",
    "categories": ["sonarr", "radarr"]
  },
  "repair": {
    "enabled": false,
    "interval": "12h",
    "run_on_start": false
  },
  "use_auth": false,
  "log_level": "info"
}
```

Full config are [here](doc/config.full.json)

<details>

<summary>
  Click Here for the full config notes
</summary>

- The `log_level` key is used to set the log level of the application. The default value is `info`. log level can be set to `debug`, `info`, `warn`, `error`
- The `max_cache_size` key is used to set the maximum number of infohashes that can be stored in the availability cache. This is used to prevent round trip to the debrid provider when using the proxy/Qbittorrent. The default value is `1000`
- The `allowed_file_types` key is an array of allowed file types that can be downloaded. By default, all movie, tv show and music file types are allowed
- The `use_auth` is used to enable basic authentication for the UI. The default value is `false`
- The `discord_webhook_url` is used to send notifications to discord
- The `min_file_size` and `max_file_size` keys are used to set the minimum and maximum file size of the torrents that can be downloaded. The default value is `0` and `0` respectively. No min/max file size will be set

##### Debrid Config
- The `debrids` key is an array of debrid providers
- The `name` key is the name of the debrid provider
- The `host` key is the API endpoint of the debrid provider
- The `api_key` key is the API key of the debrid provider. This can be comma separated for multiple API keys
- The `download_api_keys` key is the API key of the debrid provider. By default, this is the same as the `api_key` key. This is used to download the torrents. This is an array of API keys. This is useful for those using multiple api keys. The API keys are used to download the torrents.
- The `folder` key is the folder where your debrid folder is mounted(webdav, rclone, zurg etc). e.g `data/realdebrid/torrents/`, `/media/remote/alldebrid/magnets/`
- The `rate_limit` key is the rate limit of the debrid provider(null by default)
- The `download_uncached` bool key is used to download uncached torrents(disabled by default)
- The `check_cached` bool key is used to check if the torrent is cached(disabled by default)
- The `use_webdav` is used to create a webdav server for the debrid Read the [webdav](#webdav) section for more information

- The `use_webdav` bool key is used to create a webdav server for the debrid provider. The default value is `false`. Read the [webdav](#webdav) section for more information

##### Repair Config
The `repair` key is used to enable the repair worker
- The `enabled` key is used to enable the repair worker
- The `interval` key is the interval in either minutes, seconds, hours, days. Use any of this format, e.g 12:00, 5:00, 1h, 1d, 1m, 1s.
- The `run_on_start` key is used to run the repair worker on start
- The `use_webdav` key is used to enable the webdav server for the repair worker. The default value is `false`. Read the [webdav](#webdav) section for more information
- The `zurg_url` is the url of the zurg server. Typically `http://localhost:9999` or `http://zurg:9999`
- The `auto_process` is used to automatically process the repair worker. This will delete broken symlinks and re-search for missing files

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
- THe `cleanup` key is used to cleanup your arr queues. This is usually for removing dangling queues(downloads that all the files have been import, sometimes, some incomplete season packs)

</details>

### Repair Worker

The repair worker is a simple worker that checks for missing files in the Arrs(Sonarr, Radarr, etc). It's particularly useful for files either deleted by the Debrid provider or files with bad symlinks.

**Notes**

- For those using the webdav server, set the `use_webdav` key to `true` in the debrid provider config. This will speed up the repair process, exponentially.
- For those using zurg, set the `zurg_url` under repair config. This will speed up the repair process, exponentially.

- Search for broken symlinks/files
- Search for missing files
- Search for deleted/unreadable files


### WebDAV

URL: `http://localhost:8282/webdav` or `http://<ip>:8080/webdav`
The webdav server is a simple webdav server that allows you to access your debrid files over the web.While most(if not all) debrid providers have their own webdav server, this is useful for fast access to your debrid files. The webdav server is disabled by default. You can disable it by setting the `use_webdav` key to `false` in the config file of the debrid provider. The webdav server listens on port `8080` by default.
##### WebDAV Config
You can set per-debrid provider webdav config in the debrid provider config or globally in the config file using "webdav" key

You can use the webdav server with media players like Infuse, VidHub or mount it locally with Rclone(See [here](https://rclone.org/webdav/)). A sample rclone file is [here](doc/rclone.conf)

- The `torrents_refresh_interval` key is used to set the interval in to refresh the torrents. The default value is `15s`. E,g `15s`, `1m`, `1h`, `1d`
- The `download_links_refresh_interval` key is used to set the interval in to refresh the download links. The default value is `40m`. E,g `15s`, `1m`, `1h`, `1d`
- The `workers` key is the maximum number of goroutines for the webdav server. The default value is your CPU cores x 50. This is useful for limiting the number of concurrent requests to the webdav server.
  - The `folder_naming` key is used to set the folder naming convention. The default value is `original_no_ext`. The available options are:
    - `original_no_ext`: The original file name without the extension
    - `original`: The original file name with the extension
    - `filename`: The torrent filename
    - `filename_no_ext`: The torrent filename without the extension
    - `id`: The torrent id
- The `auto_expire_links_after` Download links are deemed old after this time. The default value is `3d`. E,g `15s`, `1m`, `1h`, `1d`
- The `rc_url`, `rc_user`, `rc_pass` keys are used to trigger a vfs refresh on your rclone. This speeds up the process of getting the files. This is useful for rclone users. T


### Changelog

- View the [CHANGELOG.md](CHANGELOG.md) for the latest changes


### TODO
- [x] A proper name!!!!
- [x] Debrid
  - [x] Add more Debrid Providers

- [x] Qbittorrent
  - [x] Add more Qbittorrent features
  - [x] Persist torrents on restart/server crash
- [ ] Add tests
