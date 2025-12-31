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

func TestMkdirat_Basic(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	if err := mkdirat(dirFd, "newdir", 0o755); err != nil {
		t.Fatalf("mkdirat failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "newdir"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMkdirat_NestedPath(t *testing.T) {
	dir := t.TempDir()
	// Create intermediate directory
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	if err := mkdirat(dirFd, "a/b", 0o755); err != nil {
		t.Fatalf("mkdirat nested failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "a", "b"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestMkdirat_IntermediateSymlinkBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create /tmp/someplace outside the sandbox
	escape := t.TempDir()
	// Create symlink inside sandbox pointing to escape location
	if err := os.Symlink(escape, filepath.Join(dir, "escape")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Attempt to create a directory through the symlink
	err = mkdirat(dirFd, "escape/newdir", 0o755)
	if err == nil {
		t.Fatal("mkdirat through symlink should fail")
	}

	// Verify that no directory was created in the escape directory
	if _, statErr := os.Stat(filepath.Join(escape, "newdir")); statErr == nil {
		t.Fatal("directory was created in escape location")
	}
}

func TestMkdirat_DotDotRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, "../escape", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, "/tmp/escape", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_EmptyPathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, "", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestMkdirat_DotPathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, ".", 0o755)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid for '.', got %v", err)
	}
}

func TestMkdirat_ExistingDirectoryFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "existing"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, "existing", 0o755)
	if err != os.ErrExist {
		t.Errorf("expected os.ErrExist, got %v", err)
	}
}

func TestMkdirat_ParentNotExistsFails(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	err = mkdirat(dirFd, "nonexistent/newdir", 0o755)
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStat_BasicFile(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	content := []byte("hello")
	if err := os.WriteFile(testFile, content, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fs, err := stat(dirFd, "test.txt", 0)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
	if fs.size != uint64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), fs.size)
	}
}

func TestStat_Directory(t *testing.T) {
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

	fs, err := stat(dirFd, "subdir", 0)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if fs.filetype != int8(fileTypeDirectory) {
		t.Errorf("expected directory, got filetype %d", fs.filetype)
	}
}

func TestStat_CurrentDir(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fs, err := stat(dirFd, ".", 0)
	if err != nil {
		t.Fatalf("stat . failed: %v", err)
	}

	if fs.filetype != int8(fileTypeDirectory) {
		t.Errorf("expected directory, got filetype %d", fs.filetype)
	}
}

func TestStat_NestedPath(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}
	testFile := filepath.Join(nested, "test.txt")
	if err := os.WriteFile(testFile, []byte("nested"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fs, err := stat(dirFd, "a/b/test.txt", 0)
	if err != nil {
		t.Fatalf("stat nested failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
}

func TestStat_SymlinkNoFollow(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
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

	// Without SYMLINK_FOLLOW, should stat the symlink itself
	fs, err := stat(dirFd, "link", 0)
	if err != nil {
		t.Fatalf("stat symlink failed: %v", err)
	}

	if fs.filetype != int8(fileTypeSymbolicLink) {
		t.Errorf("expected symbolic link, got filetype %d", fs.filetype)
	}
}

func TestStat_SymlinkFollow(t *testing.T) {
	dir := t.TempDir()
	content := []byte("content")
	testFile := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(testFile, content, 0o644); err != nil {
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

	// With SYMLINK_FOLLOW, should stat the target file
	fs, err := stat(dirFd, "link", lookupFlagsSymlinkFollow)
	if err != nil {
		t.Fatalf("stat symlink with follow failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
	if fs.size != uint64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), fs.size)
	}
}

func TestStat_IntermediateSymlinkBlocked(t *testing.T) {
	dir := t.TempDir()
	escape := t.TempDir()
	// Create a file in the escape directory
	testFile := filepath.Join(escape, "secret.txt")
	if err := os.WriteFile(testFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	// Create symlink inside sandbox pointing to escape location
	if err := os.Symlink(escape, filepath.Join(dir, "escape")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Attempt to stat through the symlink should fail
	_, err = stat(dirFd, "escape/secret.txt", 0)
	if err == nil {
		t.Fatal("stat through symlink should fail")
	}
}

func TestStat_DotDotRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = stat(dirFd, "../etc/passwd", 0)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestStat_AbsolutePathRejected(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = stat(dirFd, "/etc/passwd", 0)
	if err != os.ErrInvalid {
		t.Errorf("expected os.ErrInvalid, got %v", err)
	}
}

func TestStat_NonExistent(t *testing.T) {
	dir := t.TempDir()
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	_, err = stat(dirFd, "nonexistent.txt", 0)
	if err != os.ErrNotExist {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStat_SymlinkFollowEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create a symlink pointing outside the sandbox
	escapeLink := filepath.Join(dir, "escape")
	if err := os.Symlink("/etc/passwd", escapeLink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// With SYMLINK_FOLLOW, should NOT follow to /etc/passwd (sandbox escape)
	// The symlink resolves to "etc/passwd" relative to sandbox root, which doesn't exist
	_, err = stat(dirFd, "escape", lookupFlagsSymlinkFollow)
	if err == nil {
		t.Fatal("stat with symlink follow should fail for escape symlink")
	}
}

func TestStat_SymlinkChainWithDotDotEscapeBlocked(t *testing.T) {
	dir := t.TempDir()
	// Create: root/subdir/link -> ../../etc/passwd
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	escapeLink := filepath.Join(subdir, "link")
	if err := os.Symlink("../../etc/passwd", escapeLink); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// With SYMLINK_FOLLOW, should NOT follow (escapes via ..)
	_, err = stat(dirFd, "subdir/link", lookupFlagsSymlinkFollow)
	if err == nil {
		t.Fatal("stat with symlink follow should fail for .. escape")
	}
	if err != os.ErrPermission {
		t.Errorf("expected os.ErrPermission, got %v", err)
	}
}

func TestOpenat_IntermediateSymlinkAllowed(t *testing.T) {
	dir := t.TempDir()
	// Structure:
	// root/
	//   target_dir/
	//     file.txt
	//   link -> target_dir   (symlink to directory inside sandbox)
	targetDir := filepath.Join(dir, "target_dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create target_dir: %v", err)
	}
	testFile := filepath.Join(targetDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("through link"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("target_dir", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Access file through symlink directory
	file, err := openat(dirFd, "link/file.txt", 0, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat through intermediate symlink failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "through link" {
		t.Errorf("got %q, want %q", string(content), "through link")
	}
}

func TestOpenat_IntermediateSymlinkWithDotDotAllowed(t *testing.T) {
	dir := t.TempDir()
	// Structure:
	// root/
	//   target_dir/
	//     file.txt
	//   subdir/
	//     link -> ../target_dir   (symlink using .. but staying in sandbox)
	targetDir := filepath.Join(dir, "target_dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create target_dir: %v", err)
	}
	testFile := filepath.Join(targetDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("via dotdot"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	linkPath := filepath.Join(subdir, "link")
	if err := os.Symlink("../target_dir", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	rights := uint64(RightsFdRead)
	file, err := openat(dirFd, "subdir/link/file.txt", 0, 0, 0, rights)
	if err != nil {
		t.Fatalf("openat through symlink with .. failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "via dotdot" {
		t.Errorf("got %q, want %q", string(content), "via dotdot")
	}
}

func TestStat_IntermediateSymlinkAllowed(t *testing.T) {
	dir := t.TempDir()
	// Structure:
	// root/
	//   target_dir/
	//     file.txt
	//   link -> target_dir
	targetDir := filepath.Join(dir, "target_dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create target_dir: %v", err)
	}
	testFile := filepath.Join(targetDir, "file.txt")
	content := []byte("stat through link")
	if err := os.WriteFile(testFile, content, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("target_dir", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	fs, err := stat(dirFd, "link/file.txt", 0)
	if err != nil {
		t.Fatalf("stat through intermediate symlink failed: %v", err)
	}

	if fs.filetype != int8(fileTypeRegularFile) {
		t.Errorf("expected regular file, got filetype %d", fs.filetype)
	}
}

func TestMkdirat_IntermediateSymlinkAllowed(t *testing.T) {
	dir := t.TempDir()
	// Structure:
	// root/
	//   target_dir/
	//   link -> target_dir
	targetDir := filepath.Join(dir, "target_dir")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatalf("failed to create target_dir: %v", err)
	}
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink("target_dir", linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Create directory through symlink
	if err := mkdirat(dirFd, "link/newdir", 0o755); err != nil {
		t.Fatalf("mkdirat through intermediate symlink failed: %v", err)
	}

	// Verify the directory was created in the actual target
	info, err := os.Stat(filepath.Join(targetDir, "newdir"))
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestOpenat_ChainedIntermediateSymlinks(t *testing.T) {
	dir := t.TempDir()
	// Structure:
	// root/
	//   a/
	//     b/
	//       file.txt
	//   link1 -> a
	//   link2 -> link1/b
	if err := os.MkdirAll(filepath.Join(dir, "a/b"), 0o755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	testFile := filepath.Join(dir, "a/b/file.txt")
	if err := os.WriteFile(testFile, []byte("chained"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.Symlink("a", filepath.Join(dir, "link1")); err != nil {
		t.Fatalf("failed to create link1: %v", err)
	}
	if err := os.Symlink("link1/b", filepath.Join(dir, "link2")); err != nil {
		t.Fatalf("failed to create link2: %v", err)
	}
	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	file, err := openat(dirFd, "link2/file.txt", 0, 0, 0, uint64(RightsFdRead))
	if err != nil {
		t.Fatalf("openat through chained symlinks failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "chained" {
		t.Errorf("got %q, want %q", string(content), "chained")
	}
}

func TestOpenat_PermissionBypass_DotDot(t *testing.T) {
	dir := t.TempDir()

	// Create a directory "locked" with no permissions (000).
	lockedDir := filepath.Join(dir, "locked")
	if err := os.Mkdir(lockedDir, 0o700); err != nil {
		t.Fatalf("failed to create locked dir: %v", err)
	}

	// Create a target file "target.txt" in the root.
	targetFile := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	// Remove permissions from "locked".
	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatalf("failed to chmod locked dir: %v", err)
	}
	t.Cleanup(func() {
		os.Chmod(lockedDir, 0o700)
	})

	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Attempt to open "locked/../target.txt".
	// POSIX: traversing "locked" requires +x permission. Since it is 000, this should FAIL.
	_, err = openat(dirFd, "locked/../target.txt", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat bypassed permission check on 'locked' directory")
	}
}

func TestOpenat_NonExistent_DotDot(t *testing.T) {
	dir := t.TempDir()

	targetFile := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}

	dirFd, err := os.Open(dir)
	if err != nil {
		t.Fatalf("failed to open dir: %v", err)
	}
	defer dirFd.Close()

	// Attempt to open "nonexistent/../target.txt".
	// POSIX: Should fail because "nonexistent" does not exist.
	_, err = openat(dirFd, "nonexistent/../target.txt", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat bypassed existence check on 'nonexistent' directory")
	}
}

func TestOpenat_DotDotEscapeSandboxBlocked(t *testing.T) {
	// Structure (outside sandbox):
	// tmp/
	//   outside.txt      <- file outside sandbox
	//   sandbox/         <- sandbox root
	//     subdir/
	tmpDir := t.TempDir()
	outsideFile := filepath.Join(tmpDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside content"), 0o644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	sandboxRoot := filepath.Join(tmpDir, "sandbox")
	if err := os.Mkdir(sandboxRoot, 0o755); err != nil {
		t.Fatalf("failed to create sandbox root: %v", err)
	}

	subdir := filepath.Join(sandboxRoot, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	dirFd, err := os.Open(sandboxRoot)
	if err != nil {
		t.Fatalf("failed to open sandbox root: %v", err)
	}
	defer dirFd.Close()

	// Attack: "subdir/../../outside.txt" should fail (escapes sandbox)
	_, err = openat(dirFd, "subdir/../../outside.txt", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat allowed sandbox escape via subdir/../..")
	}
}

func TestOpenat_TrailingDotDotBlocked(t *testing.T) {
	// Structure:
	// tmp/
	//   outside.txt      <- file outside sandbox
	//   sandbox/         <- sandbox root
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "outside.txt"), []byte("outside"), 0o644); err != nil {
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
	_, err = openat(dirFd, "./..", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat allowed escape via ./..")
	}
}

func TestOpenat_SubdirTrailingDotDotBlocked(t *testing.T) {
	// Structure:
	// tmp/
	//   outside.txt
	//   sandbox/
	//     subdir/
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	sandboxRoot := filepath.Join(tmpDir, "sandbox")
	if err := os.Mkdir(sandboxRoot, 0o755); err != nil {
		t.Fatalf("failed to create sandbox root: %v", err)
	}
	subdir := filepath.Join(sandboxRoot, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	dirFd, err := os.Open(sandboxRoot)
	if err != nil {
		t.Fatalf("failed to open sandbox root: %v", err)
	}
	defer dirFd.Close()

	// Attack: "subdir/../.." - should fail (escapes sandbox)
	_, err = openat(dirFd, "subdir/../..", 0, 0, 0, uint64(RightsFdRead))
	if err == nil {
		t.Fatal("openat allowed escape via subdir/../..")
	}
}
