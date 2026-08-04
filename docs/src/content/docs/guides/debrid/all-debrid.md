---
title: All Debrid Setup
description: Configure All Debrid provider.
---

All Debrid is a supported Debrid provider.

## Configuration

```json
{
  "debrids": [
    {
      "provider": "alldebrid",
      "name": "All Debrid",
      "api_key": "YOUR_API_KEY"
    }
  ]
}
```

Get your API key from the All Debrid dashboard.

All configuration options from [Real Debrid](./real-debrid/) apply (rate limits, workers, proxy, etc.).

See [Configuration Reference](../configuration/#debrid-providers) for full options.

## Slot Management

AllDebrid has a limit of ~5000 active torrents. Use `slot_strategy` to automatically manage slots:

### Strategies

- **`remove_oldest`**: Before adding a new torrent, removes the oldest one on
  this AllDebrid account if the limit is reached. This looks at the whole
  account, not just torrents Decypharr added — if you share this account with
  another app, or added torrents to it directly on AllDebrid's own site,
  `remove_oldest` can remove those too. It only ever acts on the account tied
  to this `api_key`; if you configure more than one AllDebrid account in
  Decypharr, each manages its own slot limit independently.
- **`remove_after_add`**: Once a torrent finishes downloading — cached or not
  — removes it from AllDebrid to free the slot. File links remain functional —
  streaming still works. If links expire later, the repair system re-inserts
  the torrent and this strategy frees the slot again, the same as on first
  download.

### Configuration

```json
{
  "debrids": [
    {
      "provider": "alldebrid",
      "name": "All Debrid",
      "api_key": "YOUR_API_KEY",
      "slot_strategy": "remove_oldest",
      "limit": 5000
    }
  ]
}
```

The `limit` field defines the maximum number of torrents. It is optional — when
omitted, AllDebrid's own limit of 5000 is used. Set it lower to stay under a
self-imposed cap. `minimum_free_slot` reserves slots.
