# qBittorrent Configuration

DecyphArr emulates a qBittorrent instance to integrate with Arr applications. This section explains how to configure the qBittorrent settings in your `config.json` file.

## Basic Configuration

The qBittorrent functionality is configured under the `qbittorrent` key:

```json
"qbittorrent": {
  "port": "8282",
  "download_folder": "/mnt/symlinks/",
  "categories": ["sonarr", "radarr", "lidarr"],
  "refresh_interval": 5
}
```

### Configuration Options
#### Essential Settings

- `port`: The port on which the qBittorrent API will listen (default: 8282)
- `download_folder`: The folder where symlinks or downloaded files will be placed
- `categories`: An array of categories to organize downloads (usually matches your Arr applications)

#### Advanced Settings

- `refresh_interval`: How often (in seconds) to refresh the Arrs Monitored Downloads (default: 5)

#### Categories
Categories help organize your downloads and match them to specific Arr applications. Typically, you'll want to configure categories that match your Sonarr, Radarr, or other Arr applications:

```json
"categories": ["sonarr", "radarr", "lidarr", "readarr"]
```

When setting up your Arr applications to connect to DecyphArr, you'll specify these same category names.

#### Download Folder

The `download_folder` setting specifies where DecyphArr will place downloaded files or create symlinks:

```json
"download_folder": "/mnt/symlinks/"
```

This folder should be:

- Accessible to DecyphArr
- Accessible to your Arr applications
- Have sufficient space if downloading files locally


#### Port Configuration
The `port` setting determines which port the qBittorrent API will listen on:

```json
"port": "8282"
```

Ensure this port:

- Is not used by other applications
- Is accessible to your Arr applications
- Is properly exposed if using Docker (see the Docker Compose example in the Installation guide)

#### Refresh Interval
The refresh_interval setting controls how often DecyphArr checks for updates from your Arr applications:

```json
"refresh_interval": 5
```


This value is in seconds. Lower values provide more responsive updates but may increase CPU usage.