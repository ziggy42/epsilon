//go:build unix

package wasip1

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fsEntry describes a filesystem entry to create in a test.
type fsEntry struct {
	path    string // relative path from root
	content string // file content (ignored for dirs/links)
	link    string // symlink target (if non-empty, creates a symlink)
	isDir   bool   // true = directory
}

// dir creates a directory entry.
func dir(path string) fsEntry {
	return fsEntry{path: path, isDir: true}
}

// file creates a file entry with the given content.
func file(path, content string) fsEntry {
	return fsEntry{path: path, content: content}
}

// link creates a symlink entry pointing to target.
func link(path, target string) fsEntry {
	return fsEntry{path: path, link: target}
}

// testFS creates a filesystem structure for testing and returns the root
// directory path and os.File. The caller must close the returned *os.File.
// Parent directories are created automatically.
func testFS(t *testing.T, entries ...fsEntry) (string, *os.File) {
	t.Helper()
	root := t.TempDir()

	for _, e := range entries {
		fullPath := filepath.Join(root, e.path)
		parentDir := filepath.Dir(fullPath)

		// Ensure parent directories exist
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			t.Fatalf("failed to create parent dirs for %s: %v", e.path, err)
		}

		switch {
		case e.link != "":
			if err := os.Symlink(e.link, fullPath); err != nil {
				t.Fatalf("failed to create symlink %s: %v", e.path, err)
			}
		case e.isDir:
			if err := os.Mkdir(fullPath, 0o755); err != nil {
				t.Fatalf("failed to create dir %s: %v", e.path, err)
			}
		default:
			if err := os.WriteFile(fullPath, []byte(e.content), 0o644); err != nil {
				t.Fatalf("failed to create file %s: %v", e.path, err)
			}
		}
	}

	dirFd, err := os.Open(root)
	if err != nil {
		t.Fatalf("failed to open root: %v", err)
	}

	return root, dirFd
}

func TestOpenat_BasicFile(t *testing.T) {
	_, dirFd := testFS(t, file("test.txt", "hello"))
	defer dirFd.Close()

	f, err := openat(dirFd, "test.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("got %q, want %q", string(content), "hello")
	}
}

func TestOpenat_NestedPath(t *testing.T) {
	_, dirFd := testFS(t, file("a/b/c/test.txt", "nested"))
	defer dirFd.Close()

	f, err := openat(dirFd, "a/b/c/test.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "nested" {
		t.Errorf("got %q, want %q", string(content), "nested")
	}
}

func TestOpenat_Directory(t *testing.T) {
	_, dirFd := testFS(t, dir("subdir"))
	defer dirFd.Close()

	flags := int32(oFlagsDirectory)
	f, err := openat(dirFd, "subdir", false, flags, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_TrailingSlashOnDirectory(t *testing.T) {
	_, dirFd := testFS(t, dir("subdir"))
	defer dirFd.Close()

	f, err := openat(dirFd, "subdir/", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen with trailing slash on directory failed: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_TrailingSlashOnFile(t *testing.T) {
	_, dirFd := testFS(t, file("file.txt", "content"))
	defer dirFd.Close()

	f, err := openat(dirFd, "file.txt/", false, 0, 0, uint64(RightsFdRead))
	if err == nil {
		f.Close()
		t.Fatal("pathOpen with trailing slash on file should fail")
	}
}

func TestOpenat_CreateFile(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	flags := int32(oFlagsCreat)
	f, err := openat(dirFd, "file.txt", false, flags, 0, uint64(RightsFdWrite))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("created")); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
}

func TestOpenat_CreateExclusive(t *testing.T) {
	_, dirFd := testFS(t, file("exists.txt", "existing"))
	defer dirFd.Close()

	flags := int32(oFlagsCreat | oFlagsExcl)
	f, err := openat(dirFd, "exists.txt", false, flags, 0, uint64(RightsFdWrite))
	if err == nil {
		f.Close()
		t.Fatal("expected error for O_CREAT|O_EXCL on existing file")
	}
	if err != os.ErrExist {
		t.Errorf("expected os.ErrExist, got %v", err)
	}
}

func TestOpenat_Truncate(t *testing.T) {
	root, dirFd := testFS(t, file("trunc.txt", "content"))
	defer dirFd.Close()

	flags := int32(oFlagsTrunc)
	f, err := openat(dirFd, "trunc.txt", false, flags, 0, uint64(RightsFdWrite))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	f.Close()

	content, err := os.ReadFile(filepath.Join(root, "trunc.txt"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(content))
	}
}

func TestOpenat_SymlinkEscapeBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("escape", "/etc"))
	defer dirFd.Close()

	_, err := openat(dirFd, "escape", false, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("expected error when opening symlink with O_NOFOLLOW")
	}
}

func TestOpenat_SymlinkFollowAllowed(t *testing.T) {
	_, dirFd := testFS(t,
		file("real.txt", "real content"),
		link("link", "real.txt"),
	)
	defer dirFd.Close()

	f, err := openat(dirFd, "link", true, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen with symlink follow failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "real content" {
		t.Errorf("got %q, want %q", string(content), "real content")
	}
}

func TestOpenat_SymlinkFollowEscapeBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("escape", "/etc/passwd"))
	defer dirFd.Close()

	f, err := openat(dirFd, "escape", true, 0, 0, uint64(RightsFdRead))
	if err == nil {
		f.Close()
		t.Fatal("symlink escape succeeded with SYMLINK_FOLLOW")
	}
}

func TestOpenat_SymlinkWithDotDotInsideSandbox(t *testing.T) {
	_, dirFd := testFS(t,
		file("f.txt", "parent content"),
		link("subdir/link", "../f.txt"),
	)
	defer dirFd.Close()

	f, err := openat(dirFd, "subdir/link", true, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed for symlink with .. inside sandbox: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "parent content" {
		t.Errorf("got %q, want %q", string(content), "parent content")
	}
}

func TestOpenat_SymlinkCrossDirectory(t *testing.T) {
	_, dirFd := testFS(t,
		file("a/b/c/f.txt", "cross dir content"),
		link("a/d/e/link", "../../b/c/f.txt"),
	)
	defer dirFd.Close()

	f, err := openat(dirFd, "a/d/e/link", true, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "cross dir content" {
		t.Errorf("got %q, want %q", string(content), "cross dir content")
	}
}

func TestOpenat_SymlinkWithDotDotEscapeBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("subdir/link", "../../etc/passwd"))
	defer dirFd.Close()

	_, err := openat(dirFd, "subdir/link", true, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("symlink escape via .. succeeded")
	}
}

func TestOpenat_IntermediateSymlinkBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("escape_dir", "/etc"))
	defer dirFd.Close()

	_, err := openat(dirFd, "escape_dir/passwd", true, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("traversing through symlink succeeded")
	}
}

func TestOpenat_DotDotRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := openat(dirFd, "../etc/passwd", false, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_AbsolutePathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := openat(dirFd, "/etc/passwd", false, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_EmptyPathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := openat(dirFd, "", false, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_AppendFlag(t *testing.T) {
	root, dirFd := testFS(t, file("append.txt", "original"))
	defer dirFd.Close()

	flags := int32(fdFlagsAppend)
	f, err := openat(dirFd, "append.txt", false, 0, flags, uint64(RightsFdWrite))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	if _, err := f.Write([]byte(" appended")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	f.Close()

	content, err := os.ReadFile(filepath.Join(root, "append.txt"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "original appended" {
		t.Errorf("got %q, want %q", string(content), "original appended")
	}
}

func TestOpenat_NonExistent(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := openat(dirFd, "nonexistent.txt", false, 0, 0, uint64(RightsFdRead))
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestOpenat_CurrentDir(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	flags := int32(oFlagsDirectory)
	f, err := openat(dirFd, ".", false, flags, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen . failed: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMkdirat_Basic(t *testing.T) {
	root, dirFd := testFS(t)
	defer dirFd.Close()

	if err := mkdirat(dirFd, "newdir", 0o755); err != nil {
		t.Fatalf("mkdirat failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "newdir"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMkdirat_NestedPath(t *testing.T) {
	root, dirFd := testFS(t, dir("a"))
	defer dirFd.Close()

	if err := mkdirat(dirFd, "a/b", 0o755); err != nil {
		t.Fatalf("mkdirat nested failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "a", "b"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMkdirat_IntermediateSymlinkBlocked(t *testing.T) {
	escape := t.TempDir()
	_, dirFd := testFS(t, link("escape", escape))
	defer dirFd.Close()

	err := mkdirat(dirFd, "escape/newdir", 0o755)
	if err == nil {
		t.Fatal("mkdirat through symlink should fail")
	}

	if _, statErr := os.Stat(filepath.Join(escape, "newdir")); statErr == nil {
		t.Fatal("directory was created in escape location")
	}
}

func TestMkdirat_DotDotRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	err := mkdirat(dirFd, "../escape", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_AbsolutePathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	err := mkdirat(dirFd, "/tmp/escape", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_EmptyPathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	err := mkdirat(dirFd, "", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_DotPathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	err := mkdirat(dirFd, ".", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid for '.', got %v", err)
	}
}

func TestMkdirat_ExistingDirectoryFails(t *testing.T) {
	_, dirFd := testFS(t, dir("existing"))
	defer dirFd.Close()

	err := mkdirat(dirFd, "existing", 0o755)
	if err != os.ErrExist {
		t.Errorf("expected os.ErrExist, got %v", err)
	}
}

func TestMkdirat_ParentNotExistsFails(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	err := mkdirat(dirFd, "nonexistent/newdir", 0o755)
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStat_BasicFile(t *testing.T) {
	_, dirFd := testFS(t, file("test.txt", "hello"))
	defer dirFd.Close()

	fs, err := stat(dirFd, "test.txt", false)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
	if fs.size != 5 {
		t.Errorf("expected size 5, got %d", fs.size)
	}
}

func TestStat_Directory(t *testing.T) {
	_, dirFd := testFS(t, dir("subdir"))
	defer dirFd.Close()

	fs, err := stat(dirFd, "subdir", false)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if fs.filetype != int8(fileTypeDirectory) {
		t.Errorf("expected directory, got filetype %d", fs.filetype)
	}
}

func TestStat_CurrentDir(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	fs, err := stat(dirFd, ".", false)
	if err != nil {
		t.Fatalf("stat . failed: %v", err)
	}

	if fs.filetype != int8(fileTypeDirectory) {
		t.Errorf("expected directory, got filetype %d", fs.filetype)
	}
}

func TestStat_NestedPath(t *testing.T) {
	_, dirFd := testFS(t, file("a/b/test.txt", "nested"))
	defer dirFd.Close()

	fs, err := stat(dirFd, "a/b/test.txt", false)
	if err != nil {
		t.Fatalf("stat nested failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
}

func TestStat_SymlinkNoFollow(t *testing.T) {
	_, dirFd := testFS(t,
		file("real.txt", "content"),
		link("link", "real.txt"),
	)
	defer dirFd.Close()

	fs, err := stat(dirFd, "link", false)
	if err != nil {
		t.Fatalf("stat symlink failed: %v", err)
	}

	if fs.filetype != int8(fileTypeSymbolicLink) {
		t.Errorf("expected symbolic link, got filetype %d", fs.filetype)
	}
}

func TestStat_SymlinkFollow(t *testing.T) {
	_, dirFd := testFS(t,
		file("real.txt", "content"),
		link("link", "real.txt"),
	)
	defer dirFd.Close()

	fs, err := stat(dirFd, "link", true)
	if err != nil {
		t.Fatalf("stat symlink with follow failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
	if fs.size != 7 {
		t.Errorf("expected size 7, got %d", fs.size)
	}
}

func TestStat_IntermediateSymlinkBlocked(t *testing.T) {
	escape := t.TempDir()
	data := []byte("secret")
	err := os.WriteFile(filepath.Join(escape, "secret.txt"), data, 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	_, dirFd := testFS(t, link("escape", escape))
	defer dirFd.Close()

	_, err = stat(dirFd, "escape/secret.txt", false)
	if err == nil {
		t.Fatal("stat through symlink should fail")
	}
}

func TestStat_DotDotRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := stat(dirFd, "../etc/passwd", false)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestStat_AbsolutePathRejected(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := stat(dirFd, "/etc/passwd", false)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestStat_NonExistent(t *testing.T) {
	_, dirFd := testFS(t)
	defer dirFd.Close()

	_, err := stat(dirFd, "nonexistent.txt", false)
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStat_SymlinkFollowEscapeBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("escape", "/etc/passwd"))
	defer dirFd.Close()

	_, err := stat(dirFd, "escape", true)
	if err == nil {
		t.Fatal("stat with symlink follow should fail for escape symlink")
	}
}

func TestStat_SymlinkChainWithDotDotEscapeBlocked(t *testing.T) {
	_, dirFd := testFS(t, link("subdir/link", "../../etc/passwd"))
	defer dirFd.Close()

	_, err := stat(dirFd, "subdir/link", true)
	if err == nil {
		t.Fatal("stat with symlink follow should fail for .. escape")
	}
	if err != os.ErrPermission {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

func TestOpenat_IntermediateSymlinkAllowed(t *testing.T) {
	_, dirFd := testFS(t,
		file("target_dir/file.txt", "through link"),
		link("link", "target_dir"),
	)
	defer dirFd.Close()

	f, err := openat(dirFd, "link/file.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat through intermediate symlink failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "through link" {
		t.Errorf("got %q, want %q", string(content), "through link")
	}
}

func TestOpenat_IntermediateSymlinkWithDotDotAllowed(t *testing.T) {
	_, dirFd := testFS(t,
		file("target_dir/file.txt", "via dotdot"),
		link("subdir/link", "../target_dir"),
	)
	defer dirFd.Close()

	rights := uint64(RightsFdRead)
	f, err := openat(dirFd, "subdir/link/file.txt", false, 0, 0, rights)
	if err != nil {
		t.Fatalf("openat through symlink with .. failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "via dotdot" {
		t.Errorf("got %q, want %q", string(content), "via dotdot")
	}
}

func TestStat_IntermediateSymlinkAllowed(t *testing.T) {
	_, dirFd := testFS(t,
		file("target_dir/file.txt", "stat through link"),
		link("link", "target_dir"),
	)
	defer dirFd.Close()

	fs, err := stat(dirFd, "link/file.txt", false)
	if err != nil {
		t.Fatalf("stat through intermediate symlink failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
}

func TestMkdirat_IntermediateSymlinkAllowed(t *testing.T) {
	root, dirFd := testFS(t,
		dir("target_dir"),
		link("link", "target_dir"),
	)
	defer dirFd.Close()

	if err := mkdirat(dirFd, "link/newdir", 0o755); err != nil {
		t.Fatalf("mkdirat through intermediate symlink failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "target_dir", "newdir"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_ChainedIntermediateSymlinks(t *testing.T) {
	_, dirFd := testFS(t,
		file("a/b/file.txt", "chained"),
		link("link1", "a"),
		link("link2", "link1/b"),
	)
	defer dirFd.Close()

	f, err := openat(dirFd, "link2/file.txt", false, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat through chained symlinks failed: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "chained" {
		t.Errorf("got %q, want %q", string(content), "chained")
	}
}

func TestOpenat_PermissionBypass_DotDot(t *testing.T) {
	root, dirFd := testFS(t,
		dir("locked"),
		file("target.txt", "secret"),
	)
	defer dirFd.Close()

	lockedDir := filepath.Join(root, "locked")
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("failed to chmod locked dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(lockedDir, 0o700) })

	// Attempt to open "locked/../target.txt". Traversing "locked" requires +x
	// permission. Since it is 000, this should fail.
	rights := uint64(RightsFdRead)
	f, err := openat(dirFd, "locked/../target.txt", false, 0, 0, rights)
	if err == nil {
		f.Close()
		t.Fatal("openat bypassed permission check on 'locked' directory")
	}
}

func TestOpenat_NonExistent_DotDot(t *testing.T) {
	_, dirFd := testFS(t, file("target.txt", "secret"))
	defer dirFd.Close()

	// Attempt to open "nonexistent/../target.txt". Should fail because
	// "nonexistent" does not exist.
	rights := uint64(RightsFdRead)
	f, err := openat(dirFd, "nonexistent/../target.txt", false, 0, 0, rights)
	if err == nil {
		f.Close()
		t.Fatal("openat bypassed existence check on 'nonexistent' directory")
	}
}

func TestOpenat_DotDotEscapeSandboxBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte("outside content")
	err := os.WriteFile(filepath.Join(tmpDir, "outside.txt"), data, 0o644)
	if err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	sandboxRoot := filepath.Join(tmpDir, "sandbox")
	err = os.MkdirAll(filepath.Join(sandboxRoot, "subdir"), 0o755)
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	dirFd, err := os.Open(sandboxRoot)
	if err != nil {
		t.Fatalf("failed to open sandbox root: %v", err)
	}
	defer dirFd.Close()

	// "subdir/../../outside.txt" should fail (escapes sandbox)
	rights := uint64(RightsFdRead)
	f, err := openat(dirFd, "subdir/../../outside.txt", false, 0, 0, rights)
	if err == nil {
		f.Close()
		t.Fatal("openat allowed sandbox escape via subdir/../..")
	}
}

func TestOpenat_TrailingDotDotBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte("outside")
	err := os.WriteFile(filepath.Join(tmpDir, "outside.txt"), data, 0o644)
	if err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	sandboxRoot := filepath.Join(tmpDir, "sandbox")
	if err := os.Mkdir(sandboxRoot, 0o755); err != nil {
		t.Fatalf("failed to create sandbox root: %v", err)
	}

	dirFd, err := os.Open(sandboxRoot)
	if err != nil {
		t.Fatalf("failed to open sandbox root: %v", err)
	}
	defer dirFd.Close()

	// Attack: "./.." - trailing .. should fail
	_, err = openat(dirFd, "./..", false, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat allowed escape via ./..")
	}
}

func TestOpenat_SubdirTrailingDotDotBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	data := []byte("outside")
	err := os.WriteFile(filepath.Join(tmpDir, "outside.txt"), data, 0o644)
	if err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	sandboxRoot := filepath.Join(tmpDir, "sandbox")
	err = os.MkdirAll(filepath.Join(sandboxRoot, "subdir"), 0o755)
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	dirFd, err := os.Open(sandboxRoot)
	if err != nil {
		t.Fatalf("failed to open sandbox root: %v", err)
	}
	defer dirFd.Close()

	// "subdir/../.." - should fail (escapes sandbox)
	_, err = openat(dirFd, "subdir/../..", false, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat allowed escape via subdir/../..")
	}
}

func TestUtimes_BasicFile(t *testing.T) {
	root, dirFd := testFS(t, file("test.txt", "hello"))
	defer dirFd.Close()
	atime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	mtime := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()

	fstFlags := fstFlagsAtim | fstFlagsMtim
	err := utimes(dirFd, "test.txt", atime, mtime, fstFlags, false)
	if err != nil {
		t.Fatalf("utimes failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "test.txt"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	// Note: AccessTime check depends on FS support so we only verify ModTime.
	if info.ModTime().UnixNano() != mtime {
		t.Errorf("dir Mtime: got %v, want %v", info.ModTime(), mtime)
	}
}

func TestUtimes_SymlinkFollow(t *testing.T) {
	root, dirFd := testFS(t,
		file("target.txt", "content"),
		link("link", "target.txt"),
	)
	defer dirFd.Close()
	mtime := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()

	err := utimes(dirFd, "link", 0, mtime, fstFlagsMtim, true)
	if err != nil {
		t.Fatalf("utimes with followSymlinks=true failed: %v", err)
	}

	targetInfo, err := os.Stat(filepath.Join(root, "target.txt"))
	if err != nil {
		t.Fatalf("Stat target failed: %v", err)
	}
	if targetInfo.ModTime().UnixNano() != mtime {
		t.Errorf("dir Mtime: got %v, want %v", targetInfo.ModTime(), mtime)
	}
}

func TestUtimes_SymlinkNoFollow(t *testing.T) {
	root, dirFd := testFS(t,
		file("target.txt", "content"),
		link("link", "target.txt"),
	)
	defer dirFd.Close()
	// Capture initial target time
	targetBefore, err := os.Stat(filepath.Join(root, "target.txt"))
	if err != nil {
		t.Fatalf("Stat target failed: %v", err)
	}
	mtime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()

	// No follow symlink: should update symlink itself, NOT target
	err = utimes(dirFd, "link", 0, mtime, fstFlagsMtim, false)
	if err != nil {
		t.Fatalf("utimes with followSymlinks=false failed: %v", err)
	}

	// Verify target did NOT change
	targetAfter, err := os.Stat(filepath.Join(root, "target.txt"))
	if err != nil {
		t.Fatalf("Stat target failed: %v", err)
	}
	if !targetAfter.ModTime().Equal(targetBefore.ModTime()) {
		t.Errorf("target Mtime changed")
	}

	// Verify symlink itself WAS updated
	linkInfo, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatalf("Lstat link failed: %v", err)
	}
	if linkInfo.ModTime().UnixNano() != mtime {
		t.Errorf("symlink Mtime: got %v, want %v", linkInfo.ModTime(), mtime)
	}
}

func TestUtimes_Directory(t *testing.T) {
	root, dirFd := testFS(t, dir("subdir"))
	defer dirFd.Close()

	mtime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	err := utimes(dirFd, "subdir", 0, mtime.UnixNano(), fstFlagsMtim, false)
	if err != nil {
		t.Fatalf("utimes on directory failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(root, "subdir"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.ModTime().Sub(mtime).Abs() > time.Second {
		t.Errorf("dir Mtime: got %v, want %v", info.ModTime(), mtime)
	}
}
