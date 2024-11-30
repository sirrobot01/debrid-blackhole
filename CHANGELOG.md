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

#### 0.2.1

- Fix Uncached torrents not being downloaded/downloaded
- Minor bug fixed
- Fix Race condition in the cache and file system

#### 0.2.2
- Fix name mismatch in the cache
- Fix directory mapping with mounts
- Add Support for refreshing the *arrs

#### 0.2.3

- Delete uncached items from RD
- Fail if the torrent is not cached(optional)
- Fix cache not being updated

#### 0.2.4

- Add file download support(Sequential Download)
- Fix http handler error
- Fix *arrs map failing concurrently
- Fix cache not being updated

#### 0.2.5
- Fix ContentPath not being set prior
- Rewrote Readme
- Cleaned up the code

#### 0.2.6
- Delete torrent for empty matched files
- Update Readme

#### 0.2.7

- Add support for multiple debrid providers
- Add Torbox support
- Add support for configurable debrid cache checks
- Add support for configurable debrid download uncached torrents

#### 0.3.0

- Add UI for adding torrents
- Refraction of the code
- -Fix Torbox bug
- Update CI/CD
- Update Readme