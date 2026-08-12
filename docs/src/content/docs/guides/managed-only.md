---
title: Managed-Only Mode
description: Only sync torrents added through your Arr apps.
---

Managed-only mode makes Decypharr the single source of truth for your debrid provider. When enabled:

- **External torrents are ignored** — torrents added directly on the provider (outside Sonarr/Radarr) are not synced into Decypharr
- **Managed torrents are protected** — if a managed torrent is deleted from the provider, Decypharr automatically re-adds it

All torrent management should go through your Arr apps or the Decypharr UI. Note that
a torrent added from the Decypharr UI without any Arr association has no `arr_refs`
entry — the same signal Local Cleanup uses for "unmanaged" below, so it's eligible for
cleanup like any other external torrent.

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

Removes entries no Arr app currently references: never added through one, or added through one that no longer tracks the media (deleted or upgraded there). This is useful after enabling managed-only on an existing setup — previously synced external torrents will have no Arr association.

The check reads only `arr_refs`. A category on an entry does not exempt it.

There is currently no way to exempt an individual entry from this check — Scan lists
every entry a Purge would remove (name and size, for review), but the list is
informational only: there's no per-entry action to keep one out. Review the list
carefully before purging; if it removes something you meant to keep, Decypharr simply
forgets about it — the torrent itself is untouched on the provider (see below), but
you'll need to re-add it to Decypharr manually to track it again.

Torrents are **not deleted from the provider**, only from Decypharr's local storage.

### Provider Cleanup

Removes torrents from the debrid provider that are not tracked in Decypharr. This deletes actual torrents from your provider account.

:::caution
Provider cleanup is irreversible. Scan first to review what will be deleted before purging.
:::
