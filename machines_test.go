package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSSHConfigCreatesAppendsAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	cfgPath := filepath.Join(sshDir, "config")
	privPath := filepath.Join(sshDir, "id_ed25519")

	// 1. Creates ~/.ssh/config with 0600 permissions when absent.
	added, err := ensureSSHConfig(cfgPath, home, "ssh.usejinn.com", privPath)
	if err != nil || !added {
		t.Fatalf("first call: added=%v err=%v", added, err)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "Host ssh.usejinn.com\n    IdentityFile ~/.ssh/id_ed25519\n    IdentitiesOnly yes\n") {
		t.Fatalf("unexpected config:\n%s", b)
	}

	// 2. Idempotent when Host ssh.usejinn.com already exists.
	added, err = ensureSSHConfig(cfgPath, home, "ssh.usejinn.com", privPath)
	if err != nil || added {
		t.Fatalf("second call: added=%v err=%v", added, err)
	}

	// 3. Appends cleanly to an existing config with other hosts.
	existing := "Host github.com\n    User git\n"
	if err := os.WriteFile(cfgPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	customKey := filepath.Join(sshDir, "jinn_ed25519")
	added, err = ensureSSHConfig(cfgPath, home, "ssh.usejinn.com", customKey)
	if err != nil || !added {
		t.Fatalf("append call: added=%v err=%v", added, err)
	}
	b, _ = os.ReadFile(cfgPath)
	if !strings.HasPrefix(string(b), existing+"\n# Added by jinn ssh-key init\nHost ssh.usejinn.com\n    IdentityFile ~/.ssh/jinn_ed25519\n") {
		t.Fatalf("unexpected appended config:\n%s", b)
	}
}
