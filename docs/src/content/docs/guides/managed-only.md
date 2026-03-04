---
title: Managed-Only Mode
description: Only sync torrents added through your Arr apps.
---

Managed-only mode makes Decypharr the single source of truth for your debrid provider. When enabled:

- **External torrents are ignored** — torrents added directly on the provider (outside Sonarr/Radarr) are not synced into Decypharr
- **Managed torrents are protected** — if a managed torrent is deleted from the provider, Decypharr automatically re-adds it

All torrent management should go through your Arr apps or the Decypharr UI.

## Configuration

```json
{
  "managed_only": true
}
```

Or via environment variable:

```bash
DECYPHARR_MANAGED_ONLY=true
```

:::tip
Enable this if you use your debrid account exclusively through Sonarr/Radarr and don't want external torrents cluttering Decypharr.
:::

## Auto-Reinsertion

When a managed torrent disappears from the provider (manual deletion, provider cleanup, expired slot), Decypharr automatically re-adds it using the stored magnet link.

- Max 3 retry attempts per torrent
- 5-minute cooldown between retries
- After 3 failures, the torrent is removed from Decypharr

## Maintenance

The **Settings > Maintenance** tab provides tools to clean up unmanaged entries.

### Local Cleanup

Removes entries from Decypharr's database that were not added through an Arr app. This is useful after enabling managed-only on an existing setup — previously synced external torrents will have no Arr association.

Torrents are **not deleted from the provider**, only from Decypharr's local storage.

### Provider Cleanup

Removes torrents from the debrid provider that are not tracked in Decypharr. This deletes actual torrents from your provider account.

:::caution
Provider cleanup is irreversible. Scan first to review what will be deleted before purging.
:::
