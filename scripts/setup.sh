#!/bin/bash
set -e

# Install FUSE and required dependencies
apt-get update
apt-get install -y --no-install-recommends fuse3
rm -rf /var/lib/apt/lists/*

# Create nonroot user and group
groupadd -r nonroot
useradd -r -g nonroot nonroot

# Create mount directory
mkdir -p /mnt/rclone
chown nonroot:nonroot /mnt/rclone