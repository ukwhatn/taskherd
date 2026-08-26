package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testPlatform = "darwin_arm64"

// tarGz packs contents as the taskherd binary, the way a release archive holds it.
func tarGz(t *testing.T, name string, contents []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatalf("tar header を書けない: %v", err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatalf("tar 本体を書けない: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar を閉じられない: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip を閉じられない: %v", err)
	}
	return buf.Bytes()
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// releaseServer serves one tag's archive and checksums. corrupt makes the served archive differ
// from what the checksums file promises.
func releaseServer(t *testing.T, tag string, archive []byte, corrupt bool) *httptest.Server {
	t.Helper()
	name := ArchiveName(testPlatform)
	sums := digest(archive) + "  " + name + "\n"
	served := archive
	if corrupt {
		served = append(append([]byte{}, archive...), 'x')
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + tag + "/" + name:
			_, _ = w.Write(served)
		case "/" + tag + "/checksums.txt":
			_, _ = w.Write([]byte(sums))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// installed puts a stand-in for the current binary somewhere replaceable and returns its path.
func installed(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "taskherd")
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("既存バイナリを置けない: %v", err)
	}
	return path
}

// resolve puts a path through the same symlink resolution Apply does, so the two are comparable.
func resolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return resolved
}

func applierFor(server *httptest.Server, target string) *Applier {
	return &Applier{
		BaseURL:    server.URL,
		Platform:   testPlatform,
		Executable: func() (string, error) { return target, nil },
	}
}

func TestApplyReplacesTheBinary(t *testing.T) {
	archive := tarGz(t, "taskherd", []byte("new binary"))
	server := releaseServer(t, "v1.3.0", archive, false)
	target := installed(t, "old binary")

	path, err := applierFor(server, target).Apply(context.Background(), "v1.3.0")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	// The returned path has been through EvalSymlinks, which on darwin turns /var into
	// /private/var; the target has to make the same trip before the two can be compared.
	if want := resolve(t, target); path != want {
		t.Errorf("Apply() = %q, want %q", path, want)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("置換後のバイナリを読めない: %v", err)
	}
	if string(got) != "new binary" {
		t.Errorf("中身 = %q, want %q", got, "new binary")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755（実行できないバイナリを置いても意味がない）", info.Mode().Perm())
	}
}

// A truncated or tampered download must leave the working binary exactly where it was.
func TestApplyLeavesTheBinaryAloneOnChecksumMismatch(t *testing.T) {
	archive := tarGz(t, "taskherd", []byte("new binary"))
	server := releaseServer(t, "v1.3.0", archive, true)
	target := installed(t, "old binary")

	_, err := applierFor(server, target).Apply(context.Background(), "v1.3.0")
	if err == nil {
		t.Fatal("Apply() error = nil, want checksum 不一致")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want checksum 不一致であることが分かる文言", err)
	}

	got, _ := os.ReadFile(target)
	if string(got) != "old binary" {
		t.Errorf("中身 = %q, want 元のまま", got)
	}
	assertNoLeftovers(t, filepath.Dir(target))
}

func TestApplyFailsOnAMissingTag(t *testing.T) {
	archive := tarGz(t, "taskherd", []byte("new binary"))
	server := releaseServer(t, "v1.3.0", archive, false)
	target := installed(t, "old binary")

	_, err := applierFor(server, target).Apply(context.Background(), "v9.9.9")
	if err == nil {
		t.Fatal("Apply() error = nil, want 404")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old binary" {
		t.Errorf("中身 = %q, want 元のまま", got)
	}
	assertNoLeftovers(t, filepath.Dir(target))
}

// An archive holding something other than taskherd is a wrong asset, not a new version.
func TestApplyRejectsAnArchiveWithoutTheBinary(t *testing.T) {
	archive := tarGz(t, "README.md", []byte("not a binary"))
	server := releaseServer(t, "v1.3.0", archive, false)
	target := installed(t, "old binary")

	_, err := applierFor(server, target).Apply(context.Background(), "v1.3.0")
	if err == nil {
		t.Fatal("Apply() error = nil, want taskherd が無い旨")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old binary" {
		t.Errorf("中身 = %q, want 元のまま", got)
	}
}

// A binary under /usr/local/bin cannot be replaced without sudo, and saying so beats a rename
// failing after a multi-megabyte download.
func TestApplyReportsAnUnwritableDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "taskherd")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("既存バイナリを置けない: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("ディレクトリを読み取り専用にできない: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	archive := tarGz(t, "taskherd", []byte("new binary"))
	server := releaseServer(t, "v1.3.0", archive, false)

	_, err := applierFor(server, target).Apply(context.Background(), "v1.3.0")
	if !errors.Is(err, ErrNotWritable) {
		t.Errorf("error = %v, want ErrNotWritable", err)
	}
}

// A PATH entry is often a symlink into a versioned directory; replacing the link with a file would
// leave the real binary stale and the link no longer a link.
func TestApplyFollowsASymlinkToTheRealBinary(t *testing.T) {
	realDir := t.TempDir()
	real := filepath.Join(realDir, "taskherd")
	if err := os.WriteFile(real, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("実体を置けない: %v", err)
	}
	link := filepath.Join(t.TempDir(), "taskherd")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink を張れない: %v", err)
	}

	archive := tarGz(t, "taskherd", []byte("new binary"))
	server := releaseServer(t, "v1.3.0", archive, false)

	path, err := applierFor(server, link).Apply(context.Background(), "v1.3.0")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if want := resolve(t, real); path != want {
		t.Errorf("Apply() = %q, want 実体 %q", path, want)
	}

	got, _ := os.ReadFile(real)
	if string(got) != "new binary" {
		t.Errorf("実体の中身 = %q, want 置き換わっている", got)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink が実体で上書きされている")
	}
}

// A failed update must not litter the directory the binary lives in.
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "taskherd.new-") || strings.HasPrefix(entry.Name(), ".taskherd-write-probe-") {
			t.Errorf("一時ファイルが残っている: %s", entry.Name())
		}
	}
}

func TestVerifyNeedsAnEntryForTheArchive(t *testing.T) {
	archive := []byte("payload")
	sums := digest(archive) + "  taskherd_linux_amd64.tar.gz\n"

	err := verify(archive, sums, ArchiveName(testPlatform))
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Errorf("verify() error = %v, want 該当行が無い旨", err)
	}
}

// The point of an update is that what lands is runnable. Everything else here checks bytes and
// modes; this one replaces an executable through the production path and then executes what came
// out of it, which is the property a user actually depends on.
func TestApplyLeavesARunnableBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("taskherd is not built for windows")
	}

	const newScript = "#!/bin/sh\necho new\n"
	archive := tarGz(t, "taskherd", []byte(newScript))
	server := releaseServer(t, "v1.3.0", archive, false)
	target := installed(t, "#!/bin/sh\necho old\n")

	// The old one runs before the update, so a failure afterwards is the update's doing.
	if out := run(t, target); out != "old" {
		t.Fatalf("置換前の出力 = %q, want old", out)
	}

	if _, err := applierFor(server, target).Apply(context.Background(), "v1.3.0"); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if out := run(t, target); out != "new" {
		t.Errorf("置換後の出力 = %q, want new", out)
	}
}

func run(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("%s を実行できない: %v (%s)", path, err, out)
	}
	return strings.TrimSpace(string(out))
}
