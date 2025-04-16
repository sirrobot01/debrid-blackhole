
# Debrid Providers Configuration

DecyphArr supports multiple Debrid providers. This section explains how to configure each provider in your `config.json` file.

## Basic Configuration

Each Debrid provider is configured in the `debrids` array:

```json
"debrids": [
  {
    "name": "realdebrid",
    "api_key": "your-api-key",
    "folder": "/mnt/remote/realdebrid/__all__/"
  },
  {
    "name": "alldebrid",
    "api_key": "your-api-key",
    "folder": "/mnt/remote/alldebrid/downloads/"
  }
]
```

### Provider Options

Each Debrid provider accepts the following configuration options:


#### Basic Options

- `name`: The name of the Debrid provider (realdebrid, alldebrid, debridlink, torbox)
- `host`: The API endpoint of the Debrid provider
- `api_key`: Your API key for the Debrid service (can be comma-separated for multiple keys)
- `folder`: The folder where your Debrid content is mounted (via webdav, rclone, zurg, etc.)

#### Advanced Options

- `download_api_keys`: Array of API keys used specifically for downloading torrents (defaults to the same as api_key)
- `rate_limit`: Rate limit for API requests (null by default)
- `download_uncached`: Whether to download uncached torrents (disabled by default)
- `check_cached`: Whether to check if torrents are cached (disabled by default)
- `use_webdav`: Whether to create a WebDAV server for this Debrid provider (disabled by default)
- `torrents_refresh_interval`: Interval for refreshing torrent data (e.g., `15s`, `1m`, `1h`).
- `download_links_refresh_interval`: Interval for refreshing download links (e.g., `40m`, `1h`).
- `workers`: Number of concurrent workers for processing requests.
- folder_naming: Naming convention for folders:
    - `original_no_ext`: Original file name without extension
    - `original`: Original file name with extension
    - `filename`: Torrent filename
    - `filename_no_ext`: Torrent filename without extension
    - `id`: Torrent ID
- `auto_expire_links_after`: Time after which download links will expire (e.g., `3d`, `1w`).
- `rc_url`, `rc_user`, `rc_pass`: Rclone RC configuration for VFS refreshes

### Using Multiple API Keys
For services that support it, you can provide multiple download API keys for better load balancing:

```json
{
  "name": "realdebrid",
  "api_key": "key1",
  "download_api_keys": ["key1", "key2", "key3"],
  "folder": "/mnt/remote/realdebrid/__all__/"
}


```

### Example Configuration

#### Real Debrid

```json
{
  "name": "realdebrid",
  "api_key": "your-api-key",
  "folder": "/mnt/remote/realdebrid/__all__/",
  "rate_limit": null,
  "download_uncached": false,
  "check_cached": true,
  "use_webdav": true
}
```

#### All Debrid

```json
{
  "name": "alldebrid",
  "api_key": "your-api-key",
  "folder": "/mnt/remote/alldebrid/torrents/",
  "rate_limit": null,
  "download_uncached": false,
  "check_cached": true,
  "use_webdav": true
}
```

#### Debrid Link

```json
{
  "name": "debridlink",
  "api_key": "your-api-key",
  "folder": "/mnt/remote/debridlink/torrents/",
  "rate_limit": null,
  "download_uncached": false,
  "check_cached": true,
  "use_webdav": true
}
```

#### Torbox

```json
{
  "name": "torbox",
  "api_key": "your-api-key",
  "folder": "/mnt/remote/torbox/torrents/",
  "rate_limit": null,
  "download_uncached": false,
  "check_cached": true,
  "use_webdav": true
}
```