//go:build linux || (darwin && amd64)

package hanwen

import (
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/rs/zerolog"
)

func TestChildStableAttrIsDeterministic(t *testing.T) {
	dir := &Dir{virtualPath: "/__all__/example"}

	first := dir.childStableAttr("video.mkv", fuse.S_IFREG|0644)
	second := dir.childStableAttr("video.mkv", fuse.S_IFREG|0644)

	if first != second {
		t.Fatalf("same child returned different stable attributes: first=%+v second=%+v", first, second)
	}
	if first.Ino <= 1 {
		t.Fatalf("child inode must not use a reserved value: %d", first.Ino)
	}
}

func TestChildStableAttrIncludesFullParentPath(t *testing.T) {
	firstParent := &Dir{virtualPath: "/__all__/example"}
	secondParent := &Dir{virtualPath: "/provider/example"}

	first := firstParent.childStableAttr("video.mkv", fuse.S_IFREG|0644)
	second := secondParent.childStableAttr("video.mkv", fuse.S_IFREG|0644)

	if first.Ino == second.Ino {
		t.Fatalf("same child name under different parents returned inode %d", first.Ino)
	}
}

func TestNewDirTracksCanonicalVirtualPath(t *testing.T) {
	dir := NewDir(nil, "", LevelRoot, 0, nil, zerolog.Nop(), nil)

	if got := dir.childPath("__all__"); got != "/__all__" {
		t.Fatalf("root child path = %q, want %q", got, "/__all__")
	}

	group := newDir(nil, "__all__", dir.childPath("__all__"), LevelTorrent, 0, nil, zerolog.Nop(), nil)
	child := newDir(nil, "example", group.childPath("example"), LevelFile, 0, nil, zerolog.Nop(), nil)
	if got := child.childPath("video.mkv"); got != "/__all__/example/video.mkv" {
		t.Fatalf("nested child path = %q, want %q", got, "/__all__/example/video.mkv")
	}
}
