package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// jinn update replaces this binary with the latest release of
// github.com/usejinn/jinn-cli. The release workflow signs each release:
// Ed25519 over "jinn-cli <tag>\n" followed by the release's SHA256SUMS, with
// a key kept as a GitHub secret; release-key.pem is its public half. So a
// signature vouches for one tag only, and jinn update only ever moves to a
// newer version: an old release offered as the latest is refused. Nothing is
// replaced until the signature and the binary's SHA-256 check out.

//go:embed release-key.pem
var releaseKeyPEM []byte

const (
	releasesURL = "https://github.com/usejinn/jinn-cli/releases"
	installHint = "install a release with: curl -fsSL https://github.com/usejinn/jinn-cli/releases/latest/download/install.sh | sh"
)

// buildVersion is the version this binary was built as: a release tag
// (v1.2.3) for release builds and go install, else what Go recorded.
func buildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "(built from source)"
}

var releaseTag = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// newer reports whether release tag a is a later version than tag b.
func newer(a, b string) bool {
	x, y := releaseTag.FindStringSubmatch(a), releaseTag.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		m, _ := strconv.Atoi(x[i])
		n, _ := strconv.Atoi(y[i])
		if m != n {
			return m > n
		}
	}
	return false
}

func releaseKey() (ed25519.PublicKey, error) {
	block, _ := pem.Decode(releaseKeyPEM)
	if block == nil {
		return nil, errors.New("release-key.pem holds no PEM block")
	}
	k, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("release-key.pem is not an Ed25519 key")
	}
	return pub, nil
}

// asset is the release file built for this system.
func asset() string {
	name := "jinn-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func update(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return usageError("jinn update takes no arguments")
	}
	current := buildVersion()
	if !releaseTag.MatchString(current) {
		return fmt.Errorf("jinn %s is not a release build, so it cannot update itself; %s", current, installHint)
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return fmt.Errorf("find this binary: %w", err)
	}
	key, err := releaseKey()
	if err != nil {
		return err
	}
	u := updater{http: &http.Client{Timeout: 10 * time.Minute}, releases: releasesURL, key: key, asset: asset()}
	tag, err := u.run(ctx, current, exe)
	if err != nil {
		return err
	}
	switch {
	case asJSON:
		emit(map[string]any{"from": current, "to": tag, "updated": tag != current, "path": exe})
	case tag == current:
		fmt.Printf("jinn %s is the latest release.\n", current)
	default:
		fmt.Printf("Updated %s from %s to %s.\n", exe, current, tag)
	}
	return nil
}

// updater fetches a release and checks it before it replaces a binary.
type updater struct {
	http     *http.Client
	releases string // https://github.com/usejinn/jinn-cli/releases; tests point it elsewhere
	key      ed25519.PublicKey
	asset    string
}

// run replaces exe, a release build of version current, with the latest
// release when that is newer. It returns the version exe now is.
func (u updater) run(ctx context.Context, current, exe string) (string, error) {
	tag, err := u.latest(ctx)
	if err != nil {
		return "", err
	}
	if !newer(tag, current) {
		return current, nil
	}
	bin, err := u.fetch(ctx, tag)
	if err != nil {
		return "", err
	}
	if err := replace(exe, bin); err != nil {
		return "", err
	}
	return tag, nil
}

// latest is the tag of the latest release: GitHub answers /releases/latest
// with a redirect to /releases/tag/<tag>.
func (u updater) latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.releases+"/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "jinn/"+buildVersion())
	c := *u.http
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("find the latest release: %w", err)
	}
	res.Body.Close()
	tag := path.Base(res.Header.Get("Location"))
	if res.StatusCode/100 != 3 || !releaseTag.MatchString(tag) {
		return "", fmt.Errorf("find the latest release: GitHub answered %s, not a release tag", res.Status)
	}
	return tag, nil
}

// fetch downloads the release's binary for this system, once its
// SHA256SUMS is signed by the release key for this tag and the binary
// matches it.
func (u updater) fetch(ctx context.Context, tag string) ([]byte, error) {
	base := u.releases + "/download/" + tag + "/"
	sums, err := u.get(ctx, base+"SHA256SUMS", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := u.get(ctx, base+"SHA256SUMS.sig", 1<<10)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	signed := append([]byte("jinn-cli "+tag+"\n"), sums...)
	if err != nil || !ed25519.Verify(u.key, signed, raw) {
		return nil, fmt.Errorf("release %s is not signed by Jinn's release key; nothing was changed. If Jinn changed its key, %s", tag, installHint)
	}
	var want string
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == u.asset {
			want = f[0]
		}
	}
	if want == "" {
		return nil, fmt.Errorf("release %s has no %s", tag, u.asset)
	}
	bin, err := u.get(ctx, base+u.asset, 256<<20)
	if err != nil {
		return nil, err
	}
	if got := sha256.Sum256(bin); hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("%s of release %s does not match its signed SHA256SUMS; nothing was changed", u.asset, tag)
	}
	return bin, nil
}

// get downloads a release file of at most limit bytes.
func (u updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jinn/"+buildVersion())
	res, err := u.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", path.Base(url), res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", path.Base(url), err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download %s: more than %d bytes", path.Base(url), limit)
	}
	return b, nil
}

// replace puts bin in exe's place. It writes beside exe and renames, so exe
// is never half written. Windows cannot replace a running program, but can
// rename it: the old one moves to exe.old, removed by the next update.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".jinn-update-*")
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("cannot write to %s: run the update as the user who installed jinn, for example sudo jinn update", dir)
	}
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after a rename, there is nothing left to remove
	_, err = tmp.Write(bin)
	if err == nil {
		err = tmp.Chmod(0o755)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write the new jinn: %w", err)
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return fmt.Errorf("move the old jinn aside: %w", err)
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			os.Rename(old, exe)
			return fmt.Errorf("put the new jinn in place: %w", err)
		}
		return nil
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return fmt.Errorf("put the new jinn in place: %w", err)
	}
	return nil
}
