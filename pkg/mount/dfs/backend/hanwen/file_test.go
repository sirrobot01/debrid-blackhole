//go:build linux || (darwin && amd64)

package hanwen

import (
	"testing"

	"github.com/sirrobot01/decypharr/internal/logger"
	"github.com/sirrobot01/decypharr/pkg/manager"
	"github.com/sirrobot01/decypharr/pkg/mount/dfs/config"
)

func TestNewFileUsesMetadataModificationTime(t *testing.T) {
	var managerInstance manager.Manager
	info := managerInstance.RootInfo()
	file := NewFile(nil, &config.FuseConfig{}, info, &logger.RateLimitedLogger{})

	if !file.createdAt.Equal(info.ModTime()) {
		t.Fatalf("file timestamp = %v, want metadata timestamp %v", file.createdAt, info.ModTime())
	}
}

func TestNewFileChoosesStableFallbackTime(t *testing.T) {
	info := &manager.FileInfo{}
	file := NewFile(nil, &config.FuseConfig{}, info, &logger.RateLimitedLogger{})

	if file.createdAt.IsZero() {
		t.Fatal("file timestamp must be initialized when metadata has no modification time")
	}
}
