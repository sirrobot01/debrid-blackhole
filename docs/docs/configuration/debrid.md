
# Debrid Providers Configuration

DecyphArr supports multiple Debrid providers. This section explains how to configure each provider in your `config.json` file.

## Basic Configuration

Each Debrid provider is configured in the `debrids` array:

```json
"debrids": [
  {
    "name": "realdebrid",
    "host": "https://api.real-debrid.com/rest/1.0",
    "api_key": "your-api-key",
    "folder": "/mnt/remote/realdebrid/__all__/"
  },
  {
    "name": "alldebrid",
    "host": "https://api.alldebrid.com/v4",
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


### Using Multiple API Keys
For services that support it, you can provide multiple download API keys for better load balancing:

```json
{
  "name": "realdebrid",
  "host": "https://api.real-debrid.com/rest/1.0",
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
  "host": "https://api.real-debrid.com/rest/1.0",
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
  "host": "https://api.alldebrid.com/v4",
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
  "host": "https://debrid-link.com/api/v2",
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
  "host": "https://api.torbox.com/v1",
  "api_key": "your-api-key",
  "folder": "/mnt/remote/torbox/torrents/",
  "rate_limit": null,
  "download_uncached": false,
  "check_cached": true,
  "use_webdav": true
}
```