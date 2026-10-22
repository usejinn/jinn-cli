package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// release is what a fake GitHub serves: the latest tag, and that tag's
// SHA256SUMS, its signature and the binary.
type release struct {
	tag            string
	sums, sig, bin []byte
}

// signedRelease is a release as the release workflow makes one: SHA256SUMS
// lists the binary, and the key signs "jinn-cli <tag>\n" and SHA256SUMS.
func signedRelease(key ed25519.PrivateKey, tag, asset string, bin []byte) release {
	sum := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n" + strings.Repeat("0", 64) + "  install.sh\n")
	sig := ed25519.Sign(key, append([]byte("jinn-cli "+tag+"\n"), sums...))
	return release{tag: tag, sums: sums, sig: []byte(base64.StdEncoding.EncodeToString(sig) + "\n"), bin: bin}
}

func serve(t *testing.T, r release, asset string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest":
			http.Redirect(w, req, "/tag/"+r.tag, http.StatusFound)
		case "/download/" + r.tag + "/SHA256SUMS":
			w.Write(r.sums)
		case "/download/" + r.tag + "/SHA256SUMS.sig":
			w.Write(r.sig)
		case "/download/" + r.tag + "/" + asset:
			w.Write(r.bin)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// installed writes an old jinn into a folder of its own.
func installed(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "jinn")
	if err := os.WriteFile(exe, []byte("old jinn"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// A newer release, signed for its tag, whose binary matches SHA256SUMS,
// replaces the binary; run again, it changes nothing.
func TestUpdateReplacesWithASignedRelease(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	const asset = "jinn-test-amd64"
	u := updater{http: http.DefaultClient, releases: serve(t, signedRelease(key, "v1.10.0", asset, []byte("new jinn")), asset), key: pub, asset: asset}
	exe := installed(t)
	tag, err := u.run(context.Background(), "v1.9.3", exe)
	if err != nil || tag != "v1.10.0" {
		t.Fatalf("update: %q, %v", tag, err)
	}
	got, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	if string(got) != "new jinn" || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
		t.Fatalf("after the update: %q, mode %v", got, info.Mode())
	}
	if tag, err := u.run(context.Background(), "v1.10.0", exe); err != nil || tag != "v1.10.0" {
		t.Fatalf("at the latest release: %q, %v", tag, err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".jinn-update-*")); len(left) > 0 {
		t.Fatalf("left behind: %v", left)
	}
}

// Nothing changes unless the release is newer, signed by the release key
// for its own tag, and its binary matches the signed SHA256SUMS.
func TestUpdateRefuses(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	const asset = "jinn-test-amd64"
	good := func() release { return signedRelease(key, "v2.0.0", asset, []byte("new jinn")) }
	for name, c := range map[string]struct {
		release release
		wantErr string
	}{
		"another key's signature": {signedRelease(otherKey, "v2.0.0", asset, []byte("new jinn")), "not signed by Jinn's release key"},
		"a signature for another tag": {func() release {
			r, old := good(), signedRelease(key, "v1.0.1", asset, []byte("new jinn"))
			r.sig = old.sig
			return r
		}(), "not signed by Jinn's release key"},
		"a binary that is not the one signed": {func() release { r := good(); r.bin = []byte("other jinn"); return r }(), "does not match its signed SHA256SUMS"},
		"no build for this system":            {signedRelease(key, "v2.0.0", "jinn-other-arch", []byte("new jinn")), "has no " + asset},
		"a signature that is not base64":      {func() release { r := good(); r.sig = []byte("not a signature"); return r }(), "not signed by Jinn's release key"},
		"an older release as the latest":      {signedRelease(key, "v1.0.0", asset, []byte("old release")), ""},
	} {
		t.Run(name, func(t *testing.T) {
			u := updater{http: http.DefaultClient, releases: serve(t, c.release, asset), key: pub, asset: asset}
			exe := installed(t)
			tag, err := u.run(context.Background(), "v1.0.1", exe)
			if c.wantErr == "" && (err != nil || tag != "v1.0.1") {
				t.Fatalf("want no update, got %q, %v", tag, err)
			}
			if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("want an error with %q, got %q, %v", c.wantErr, tag, err)
			}
			if got, _ := os.ReadFile(exe); string(got) != "old jinn" {
				t.Fatalf("the binary changed: %q", got)
			}
		})
	}
}

// A latest release that is not a version tag is an error, not an update.
func TestUpdateNeedsAReleaseTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tag/nightly", http.StatusFound)
	}))
	defer srv.Close()
	u := updater{http: http.DefaultClient, releases: srv.URL}
	if _, err := u.run(context.Background(), "v1.0.0", installed(t)); err == nil || !strings.Contains(err.Error(), "not a release tag") {
		t.Fatalf("got %v", err)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v0.6.0", "v0.5.4", true}, {"v0.5.10", "v0.5.9", true}, {"v1.0.0", "v0.99.99", true}, {"v0.5.4", "v0.5.4", false}, {"v0.5.3", "v0.5.4", false}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// release-key.pem is the public half of the key the release workflow
// signs with; the binary embeds it.
func TestReleaseKey(t *testing.T) {
	if k, err := releaseKey(); err != nil || len(k) != ed25519.PublicKeySize {
		t.Fatal(fmt.Sprint(len(k)), err)
	}
}
