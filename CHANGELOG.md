#### 0.1.0
- Initial Release
- Added Real Debrid Support
- Added Arrs Support
- Added Proxy Support
- Added Basic Authentication for Proxy
- Added Rate Limiting for Debrid Providers

#### 0.1.1
- Added support for "No Blackhole" for Arrs
- Added support for "Cached Only" for Proxy
- Bug Fixes

#### 0.1.2
- Bug fixes
- Code cleanup
- Get available hashes at once

#### 0.1.3

- Searching for infohashes in the xml description/summary/comments
- Added local cache support
- Added max cache size
- Rewrite blackhole.go
- Bug fixes
  - Fixed indexer getting disabled
  - Fixed blackhole not working

#### 0.1.4

- Rewrote Report log
- Fix YTS, 1337x not grabbing infohash
- Fix Torrent symlink bug


#### 0.2.0-beta

- Switch to QbitTorrent API instead of Blackhole
- Rewrote the whole codebase


#### 0.2.0
- Implement 0.2.0-beta changes
- Removed Blackhole
- Added QbitTorrent API
- Cleaned up the code