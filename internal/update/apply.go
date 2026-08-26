package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DownloadURL is where release archives live. Split from APIURL because the two are different
// hosts and only one of them is an API.
var DownloadURL = "https://github.com/" + Repo + "/releases/download"

// maxArchiveSize caps what will be pulled into memory. taskherd's archives are a few megabytes;
// this is loose enough never to matter and tight enough that a wrong URL cannot exhaust memory.
const maxArchiveSize = 64 << 20

// ErrNotWritable means the binary sits somewhere this process cannot replace it — a system
// directory, usually. It is separated out because the fix is a different install command, not a
// retry.
var ErrNotWritable = errors.New("the installed binary is not writable by this user")

// NotWritableError names the directory that could not be written to, so the caller can say which
// one without taking the error message apart.
type NotWritableError struct {
	Dir string
}

func (e *NotWritableError) Error() string {
	return fmt.Sprintf("%s: %s", ErrNotWritable, e.Dir)
}

func (e *NotWritableError) Unwrap() error { return ErrNotWritable }

// Applier replaces the running binary with a release.
type Applier struct {
	// Client is the HTTP client used to download. Nil means a default client.
	Client *http.Client
	// BaseURL overrides DownloadURL, for tests.
	BaseURL string
	// Executable resolves the path to replace. Nil means the running binary.
	Executable func() (string, error)
	// Platform is the os_arch pair naming the archive. Empty means this machine's.
	Platform string
}

// Apply downloads the release tagged tag and puts it where the running binary is.
//
// Nothing touches the filesystem until the download has been read whole and its digest matches the
// release's checksums file. The replacement is then written beside the binary it replaces and
// renamed over it, so the binary is either the old one or the new one and never a partial file.
// A process already running from the old binary keeps running: the rename swaps the directory
// entry, not the open file.
func (a *Applier) Apply(ctx context.Context, tag string) (path string, err error) {
	target, err := a.target()
	if err != nil {
		return "", err
	}
	// Checking before the download turns "no permission" into a fast, clear failure instead of one
	// that arrives after several megabytes.
	if err := writable(filepath.Dir(target)); err != nil {
		return "", err
	}

	name := ArchiveName(a.platform())
	archive, err := a.download(ctx, tag, name)
	if err != nil {
		return "", err
	}
	sums, err := a.download(ctx, tag, "checksums.txt")
	if err != nil {
		return "", err
	}
	if err := verify(archive, string(sums), name); err != nil {
		return "", err
	}

	binary, err := extract(archive)
	if err != nil {
		return "", err
	}
	if err := replace(target, binary); err != nil {
		return "", err
	}
	return target, nil
}

// ArchiveName is the release asset for one platform. It matches the name_template in
// .goreleaser.yaml, which deliberately leaves the version out so this stays a concatenation.
func ArchiveName(platform string) string { return "taskherd_" + platform + ".tar.gz" }

// Platform is the os_arch pair this build runs on.
func Platform() string { return runtime.GOOS + "_" + runtime.GOARCH }

func (a *Applier) platform() string {
	if a.Platform != "" {
		return a.Platform
	}
	return Platform()
}

// target is the file to replace: the running binary, with any symlinks resolved so that a link in
// a PATH directory is followed to the real thing rather than overwritten with a copy.
func (a *Applier) target() (string, error) {
	self := a.Executable
	if self == nil {
		self = os.Executable
	}
	path, err := self()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A path that cannot be resolved is still worth trying: the rename will say why.
		return path, nil
	}
	return resolved, nil
}

func (a *Applier) download(ctx context.Context, tag, name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/%s", a.baseURL(), tag, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build the request for %s: %w", name, err)
	}

	resp, err := a.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot download %s: the server returned %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", url, err)
	}
	return data, nil
}

// verify checks the archive against its line in the release's checksums file. A truncated download
// and a tampered one are indistinguishable here, and both have to stop the update.
func verify(archive []byte, sums, name string) error {
	want := ""
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(line)
		// The format is "<digest>  <name>", the two spaces being sha256sum's own.
		if len(fields) == 2 && fields[1] == name {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", name)
	}

	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}
	return nil
}

// extract pulls the taskherd binary out of the archive. Only that one entry is read: an archive is
// untrusted input, and walking it into the filesystem is how path traversal happens.
func extract(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("cannot read the archive: %w", err)
	}
	defer func() {
		_ = gz.Close()
	}()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read the archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "taskherd" {
			continue
		}
		binary, err := io.ReadAll(io.LimitReader(reader, maxArchiveSize))
		if err != nil {
			return nil, fmt.Errorf("cannot read taskherd out of the archive: %w", err)
		}
		return binary, nil
	}
	return nil, errors.New("the archive contains no taskherd binary")
}

// replace puts binary at path via a rename from the same directory, which is what makes the swap
// atomic and what lets it happen while the old binary is running.
func replace(path string, binary []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "taskherd.new-*")
	if err != nil {
		return fmt.Errorf("cannot create the replacement next to %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(binary); err != nil {
		return fmt.Errorf("cannot write the replacement: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("cannot fsync the replacement: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close the replacement: %w", err)
	}
	// The mode is set explicitly: CreateTemp makes the file 0600, which would leave taskherd
	// unrunnable by anyone but its owner.
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("cannot make the replacement executable: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot put the replacement in place at %s: %w", path, err)
	}
	return nil
}

// writable reports whether this process can create a file in dir, which is the permission the
// rename actually needs — the binary's own mode says nothing about it.
func writable(dir string) error {
	probe, err := os.CreateTemp(dir, ".taskherd-write-probe-*")
	if err != nil {
		return &NotWritableError{Dir: dir}
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func (a *Applier) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return http.DefaultClient
}

func (a *Applier) baseURL() string {
	if a.BaseURL != "" {
		return a.BaseURL
	}
	return DownloadURL
}
