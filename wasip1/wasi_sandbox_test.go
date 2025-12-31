//go:build unix

package wasip1

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenat_BasicFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	file, err := openat(dirFd, "test.txt", 0, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("got %q, want %q", string(content), "hello")
	}
}

func TestOpenat_NestedPath(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}
	testFile := filepath.Join(nested, "test.txt")
	if err := os.WriteFile(testFile, []byte("nested"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	file, err := openat(dirFd, "a/b/c/test.txt", 0, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "nested" {
		t.Errorf("got %q, want %q", string(content), "nested")
	}
}

func TestOpenat_Directory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	oFlags := int32(oFlagsDirectory)
	file, err := openat(dirFd, "subdir", 0, oFlags, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_TrailingSlashOnDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	file, err := openat(dirFd, "subdir/", 0, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen with trailing slash on directory failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_TrailingSlashOnFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = openat(dirFd, "file.txt/", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("pathOpen with trailing slash on file should fail")
	}
}

func TestOpenat_CreateFile(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fsRights := uint64(RightsFdRead | RightsFdWrite)
	file, err := openat(dirFd, "file.txt", 0, int32(oFlagsCreat), 0, fsRights)
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer file.Close()

	if _, err := file.Write([]byte("created")); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
}

func TestOpenat_CreateExclusive(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(testFile, []byte("existing"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	oFlags := int32(oFlagsCreat | oFlagsExcl)
	_, err = openat(dirFd, "existing.txt", 0, oFlags, 0, uint64(RightsFdWrite))

	if err == nil {
		t.Fatal("expected error for O_CREAT|O_EXCL on existing file")
	}
	if err != os.ErrExist {
		t.Errorf("expected os.ErrExist, got %v", err)
	}
}

func TestOpenat_Truncate(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "trunc.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	oFlags := int32(oFlagsTrunc)
	file, err := openat(dirFd, "trunc.txt", 0, oFlags, 0, uint64(RightsFdWrite))
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	file.Close()

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if len(content) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(content))
	}
}

func TestOpenat_SymlinkEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	escapeLink := filepath.Join(dir, "escape")
	if err := os.Symlink("/etc", escapeLink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Try to open the escape link without following symlinks
	// This should fail with ELOOP because O_NOFOLLOW is set
	_, err = openat(dirFd, "escape", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("expected error when opening symlink with O_NOFOLLOW")
	}
}

func TestOpenat_SymlinkFollowAllowed(t *testing.T) {
	dir := t.TempDir()

	// Create a file and a symlink to it within the sandbox
	testFile := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(testFile, []byte("real content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	symlink := filepath.Join(dir, "link")
	if err := os.Symlink("real.txt", symlink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Open with SYMLINK_FOLLOW flag
	lookupFlags := lookupFlagsSymlinkFollow
	file, err := openat(dirFd, "link", lookupFlags, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen with symlink follow failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "real content" {
		t.Errorf("got %q, want %q", string(content), "real content")
	}
}

func TestOpenat_SymlinkFollowEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create a symlink that points outside the sandbox
	// Even with SYMLINK_FOLLOW, this should be blocked
	escapeLink := filepath.Join(dir, "escape")
	if err := os.Symlink("/etc/passwd", escapeLink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Try to open the escape symlink WITH SYMLINK_FOLLOW
	// This MUST fail to prevent sandbox escape
	lookupFlags := lookupFlagsSymlinkFollow
	_, err = openat(dirFd, "escape", lookupFlags, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("symlink escape succeeded with SYMLINK_FOLLOW")
	}
}

func TestOpenat_SymlinkWithDotDotInsideSandbox(t *testing.T) {
	dir := t.TempDir()
	// Create this structure:
	// root/
	//   f.txt                <- target file
	//   subdir/
	//     link -> ../f.txt   <- symlink using .. that stays in sandbox
	data := []byte("parent content")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), data, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.Symlink("../f.txt", filepath.Join(subdir, "link")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// This should work: the symlink uses ".." but resolves to root/file.txt
	lookupFlags := lookupFlagsSymlinkFollow
	rights := uint64(RightsFdRead)
	file, err := openat(dirFd, "subdir/link", lookupFlags, 0, 0, rights)
	if err != nil {
		t.Fatalf("pathOpen failed for symlink with .. inside sandbox: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "parent content" {
		t.Errorf("got %q, want %q", string(content), "parent content")
	}
}

func TestOpenat_SymlinkCrossDirectory(t *testing.T) {
	dir := t.TempDir()
	// root/a/b/c/file.txt <- target, root/a/d/e/link -> ../../b/c/file.txt
	if err := os.MkdirAll(filepath.Join(dir, "a/b/c"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "a/d/e"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	data := []byte("cross dir content")
	err := os.WriteFile(filepath.Join(dir, "a/b/c/f.txt"), data, 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	linkPath := filepath.Join(dir, "a/d/e/link")
	if err := os.Symlink("../../b/c/f.txt", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	lookupFlags := lookupFlagsSymlinkFollow
	fsRights := uint64(RightsFdRead)
	file, err := openat(dirFd, "a/d/e/link", lookupFlags, 0, 0, fsRights)
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "cross dir content" {
		t.Errorf("got %q, want %q", string(content), "cross dir content")
	}
}

func TestOpenat_SymlinkWithDotDotEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	// root/subdir/link -> ../../etc/passwd (escapes sandbox)
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	linkPath := filepath.Join(subdir, "link")
	if err := os.Symlink("../../etc/passwd", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	lookupFlags := lookupFlagsSymlinkFollow
	fsRights := uint64(RightsFdRead)
	_, err = openat(dirFd, "subdir/link", lookupFlags, 0, 0, fsRights)
	if err == nil {
		t.Fatal("symlink escape via .. succeeded")
	}
}

func TestOpenat_IntermediateSymlinkBlocked(t *testing.T) {
	dir := t.TempDir()
	escapeDir := filepath.Join(dir, "escape_dir")
	if err := os.Symlink("/etc", escapeDir); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	lookupFlags := lookupFlagsSymlinkFollow
	fsRights := uint64(RightsFdRead)
	_, err = openat(dirFd, "escape_dir/passwd", lookupFlags, 0, 0, fsRights)
	if err == nil {
		t.Fatal("traversing through symlink succeeded")
	}
}

func TestOpenat_DotDotRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = openat(dirFd, "../etc/passwd", 0, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = openat(dirFd, "/etc/passwd", 0, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_EmptyPathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = openat(dirFd, "", 0, 0, 0, uint64(RightsFdRead))
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestOpenat_AppendFlag(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "append.txt")
	if err := os.WriteFile(testFile, []byte("original"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fdFlags := int32(fdFlagsAppend)
	fsRights := uint64(RightsFdWrite)
	file, err := openat(dirFd, "append.txt", 0, 0, fdFlags, fsRights)
	if err != nil {
		t.Fatalf("pathOpen failed: %v", err)
	}
	if _, err := file.Write([]byte(" appended")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	file.Close()

	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "original appended" {
		t.Errorf("got %q, want %q", string(content), "original appended")
	}
}

func TestOpenat_NonExistent(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = openat(dirFd, "nonexistent.txt", 0, 0, 0, uint64(RightsFdRead))
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestOpenat_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	oFlags := int32(oFlagsDirectory)
	file, err := openat(dirFd, ".", 0, oFlags, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("pathOpen . failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}
