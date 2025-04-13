# General Configuration

This section covers the basic configuration options for DecyphArr that apply to the entire application.

## Basic Settings

Here are the fundamental configuration options:

```json
{
  "use_auth": false,
  "log_level": "info",
  "discord_webhook_url": "",
  "min_file_size": 0,
  "max_file_size": 0,
  "allowed_file_types": [".mp4", ".mkv", ".avi", ...]
}
```

### Configuration Options

#### Log Level
The `log_level` setting determines how verbose the application logs will be:

- `debug`: Detailed information, useful for troubleshooting
- `info`: General operational information (default)
- `warn`: Warning messages
- `error`: Error messages only
- `trace`: Very detailed information, including all requests and responses


#### Authentication
The `use_auth` option enables basic authentication for the UI:

```json
"use_auth": true
```

When enabled, you'll need to provide a username and password to access the DecyphArr interface.


#### File Size Limits

You can set minimum and maximum file size limits for torrents:
```json
"min_file_size": 0,  // Minimum file size in bytes (0 = no minimum)
"max_file_size": 0   // Maximum file size in bytes (0 = no maximum)
```

#### Allowed File Types
You can restrict the types of files that DecyphArr will process by specifying allowed file extensions. This is useful for filtering out unwanted file types.

```json
"allowed_file_types": [
  ".mp4", ".mkv", ".avi", ".mov",
  ".m4v", ".mpg", ".mpeg", ".wmv",
  ".m4a", ".mp3", ".flac", ".wav"
]
```

If not specified, all movie, TV show, and music file types are allowed by default.


#### Discord Notifications
To receive notifications on Discord, add your webhook URL:
```json
"discord_webhook_url": "https://discord.com/api/webhooks/..."
```
This will send notifications for various events, such as successful downloads or errors.
