package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	jinn "usejinn.com/go"
)

// ── SSH keys ─────────────────────────────────────────────────────────

func sshKeys(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.SSHKeys(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		emit(list)
		return nil
	}
	for _, k := range list {
		fmt.Printf("%-28s %-24s %-52s %s\n", k.ID, k.Name, k.Fingerprint, k.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
	return nil
}

func sshKey(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: jinn ssh-key init|add|rm …")
	}
	switch args[0] {
	case "init":
		return sshKeyInit(ctx, args[1:])
	case "add":
		return sshKeyAdd(ctx, args[1:])
	case "rm":
		if len(args) != 2 {
			return usageError("usage: jinn ssh-key rm ssh_…")
		}
		c, err := client()
		if err != nil {
			return err
		}
		if err := c.DeleteSSHKey(ctx, args[1]); err != nil {
			return err
		}
		if asJSON {
			emit(map[string]string{"deleted": args[1]})
		}
		return nil
	default:
		return usageError("usage: jinn ssh-key init|add|rm …")
	}
}

func defaultSSHKeyName() string {
	host, _ := os.Hostname()
	name := strings.TrimSpace(host)
	if name == "" {
		return "default"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return name
}

func sshKeyInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ssh-key init", flag.ContinueOnError)
	name := fs.String("name", defaultSSHKeyName(), "name for this SSH key in Jinn")
	file := fs.String("file", "", "private key path (default ~/.ssh/id_ed25519)")
	noConfig := fs.Bool("no-config", false, "do not add Host ssh.usejinn.com to ~/.ssh/config")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("usage: jinn ssh-key init [--name NAME] [--file PATH] [--no-config]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(home, ".ssh")
	privPath := *file
	if privPath == "" {
		privPath = filepath.Join(sshDir, "id_ed25519")
	} else if strings.HasPrefix(privPath, "~/") {
		privPath = filepath.Join(home, privPath[2:])
	}
	pubPath := privPath + ".pub"
	created := false
	if _, err := os.Stat(pubPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(privPath); err == nil {
			return fmt.Errorf("%s exists without %s; pass a public key file to jinn ssh-key add", privPath, pubPath)
		}
		if err := os.MkdirAll(filepath.Dir(privPath), 0o700); err != nil {
			return err
		}
		gen := exec.CommandContext(ctx, "ssh-keygen", "-t", "ed25519", "-f", privPath, "-N", "", "-C", *name)
		gen.Stdout = os.Stderr
		gen.Stderr = os.Stderr
		if err := gen.Run(); err != nil {
			return fmt.Errorf("ssh-keygen failed: %w", err)
		}
		created = true
	}
	if err := uploadSSHKey(ctx, pubPath, *name, created); err != nil {
		return err
	}
	if !*noConfig {
		cfgPath := filepath.Join(sshDir, "config")
		added, err := ensureSSHConfig(cfgPath, home, "ssh.usejinn.com", privPath)
		if err != nil {
			return err
		}
		if !asJSON {
			if added {
				fmt.Printf("%s added Host ssh.usejinn.com to %s\n", paint(green, "✓"), cfgPath)
			} else {
				fmt.Printf("%s %s already configures ssh.usejinn.com\n", paint(green, "✓"), cfgPath)
			}
		}
	}
	return nil
}

// ensureSSHConfig appends a Host block for host pointing to privPath in
// cfgPath if no Host entry already names host. It returns true when it wrote
// a new block.
func ensureSSHConfig(cfgPath, home, host, privPath string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return false, err
	}
	var existing []byte
	if b, err := os.ReadFile(cfgPath); err == nil {
		existing = b
		if sshConfigHasHost(string(b), host) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	idFile := privPath
	if home != "" {
		if rel, err := filepath.Rel(home, privPath); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			idFile = "~/" + filepath.ToSlash(rel)
		}
	}
	if strings.ContainsAny(idFile, " \t") {
		idFile = fmt.Sprintf("%q", idFile)
	}
	block := fmt.Sprintf("# Added by jinn ssh-key init\nHost %s\n    IdentityFile %s\n    IdentitiesOnly yes\n    ServerAliveInterval 30\n", host, idFile)
	var out strings.Builder
	if len(existing) > 0 {
		out.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
	}
	out.WriteString(block)
	perm := os.FileMode(0o600)
	if info, err := os.Stat(cfgPath); err == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(cfgPath, []byte(out.String()), perm); err != nil {
		return false, err
	}
	return true, nil
}

func sshConfigHasHost(content, target string) bool {
	target = strings.ToLower(target)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.ToLower(fields[0]) != "host" {
			continue
		}
		for _, h := range fields[1:] {
			if strings.ToLower(h) == target {
				return true
			}
		}
	}
	return false
}

func sshKeyAdd(ctx context.Context, args []string) error {
	var pubPath string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pubPath = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("ssh-key add", flag.ContinueOnError)
	name := fs.String("name", defaultSSHKeyName(), "name for this SSH key in Jinn")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() == 1 && pubPath == "" {
		pubPath = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return usageError("usage: jinn ssh-key add [FILE] [--name NAME]")
	}
	if pubPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		for _, candidate := range []string{"id_ed25519.pub", "id_ecdsa.pub", "id_rsa.pub"} {
			p := filepath.Join(home, ".ssh", candidate)
			if _, err := os.Stat(p); err == nil {
				pubPath = p
				break
			}
		}
		if pubPath == "" {
			return errors.New("no SSH public key found in ~/.ssh; run jinn ssh-key init or pass a .pub file")
		}
	}
	return uploadSSHKey(ctx, pubPath, *name, false)
}

func uploadSSHKey(ctx context.Context, pubPath, name string, created bool) error {
	raw, err := os.ReadFile(pubPath)
	if err != nil {
		return err
	}
	c, err := client()
	if err != nil {
		return err
	}
	k, err := c.AddSSHKey(ctx, jinn.SSHKeyRequest{Name: name, PublicKey: strings.TrimSpace(string(raw))})
	if err != nil {
		var ae *jinn.Error
		if errors.As(err, &ae) && ae.Code == "ssh_key_exists" {
			if asJSON {
				emit(map[string]any{"status": "exists", "path": pubPath, "error": ae.Message})
			} else {
				fmt.Printf("%s %s is already registered in Jinn\n", paint(green, "✓"), pubPath)
			}
			return nil
		}
		return err
	}
	if asJSON {
		emit(k)
		return nil
	}
	if created {
		fmt.Printf("%s generated %s\n", paint(green, "✓"), strings.TrimSuffix(pubPath, ".pub"))
	}
	fmt.Printf("%s added %s (%s) %s\n", paint(green, "✓"), k.Name, paint(cyan, k.ID), paint(dim, k.Fingerprint))
	return nil
}

// ── Machines ─────────────────────────────────────────────────────────

func machines(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.Machines(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		emit(list)
		return nil
	}
	for _, m := range list {
		name := m.Name
		if name == "" {
			name = "-"
		}
		fmt.Printf("%-28s %-20s %-18s %-8s %-3s %s\n", m.ID, m.Slug, name, m.State, m.Size, m.SSH)
	}
	return nil
}

type stringFlags []string

func (s *stringFlags) String() string { return "" }
func (s *stringFlags) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func machine(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: jinn machine create|show|start|stop|heartbeat|resize|snapshot|rm …")
	}
	switch args[0] {
	case "create":
		return machineCreate(ctx, args[1:])
	case "show":
		return machineShow(ctx, args[1:])
	case "start":
		return machineStart(ctx, args[1:])
	case "stop":
		return machineStop(ctx, args[1:])
	case "heartbeat":
		return machineHeartbeat(ctx, args[1:])
	case "resize":
		return machineResize(ctx, args[1:])
	case "snapshot":
		return machineSnapshot(ctx, args[1:])
	case "rm":
		return machineRm(ctx, args[1:])
	default:
		return usageError("usage: jinn machine create|show|start|stop|heartbeat|resize|snapshot|rm …")
	}
}

func machineCreate(ctx context.Context, args []string) error {
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("machine create", flag.ContinueOnError)
	size := fs.String("size", "", "machine size: s, m, l or xl (default m)")
	img := fs.String("image", "", "starting image: img_… or base name")
	idle := fs.Int("idle", 0, "idle stop timeout in minutes (default 10)")
	timeout := fs.Int("timeout", 0, "hard timeout per wake in minutes (1 to 1440)")
	webhook := fs.String("webhook", "", "https URL for run lifecycle events")
	ref := fs.String("ref", "", "external reference")
	var env envFlags
	var setup stringFlags
	fs.Var(&env, "env", "environment variable NAME=value (repeatable)")
	fs.Var(&setup, "setup", "setup command run as root on every wake (repeatable)")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 && name == "" {
		name = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return usageError("usage: jinn machine create [NAME] [--size s|m|l|xl] [--image IMG] [--idle 10] [--timeout M] [--env K=V]… [--setup CMD]… [--webhook URL] [--ref REF]")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.CreateMachine(ctx, jinn.MachineRequest{
		Name:              name,
		Size:              *size,
		Image:             *img,
		IdleMinutes:       *idle,
		TimeoutMinutes:    *timeout,
		Setup:             setup,
		Environment:       env,
		Webhook:           *webhook,
		ExternalReference: *ref,
	})
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("%s %s %s\n", paint(green, "→"), paint(cyan, m.ID), m.Slug)
	fmt.Printf("  ssh %s\n", m.SSH)
	return nil
}

func machineShow(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn machine show NAME|SLUG|mch_…")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.Machine(ctx, args[0])
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("id:      %s\n", m.ID)
	fmt.Printf("slug:    %s\n", m.Slug)
	if m.Name != "" {
		fmt.Printf("name:    %s\n", m.Name)
	}
	fmt.Printf("state:   %s\n", m.State)
	fmt.Printf("size:    %s\n", m.Size)
	fmt.Printf("image:   %s\n", m.Image)
	fmt.Printf("idle:    %dm\n", m.IdleMinutes)
	if m.TimeoutMinutes > 0 {
		fmt.Printf("timeout: %dm\n", m.TimeoutMinutes)
	}
	if m.Webhook != "" {
		fmt.Printf("webhook: %s\n", m.Webhook)
	}
	if m.ExternalReference != "" {
		fmt.Printf("ref:     %s\n", m.ExternalReference)
	}
	if len(m.Environment) > 0 {
		fmt.Printf("env:     %d variables\n", len(m.Environment))
	}
	if len(m.Setup) > 0 {
		fmt.Printf("setup:   %d commands\n", len(m.Setup))
	}
	fmt.Printf("ssh:     ssh %s\n", m.SSH)
	if m.ActiveRun != "" {
		fmt.Printf("run:     %s (leases=%d)\n", m.ActiveRun, m.ActiveConnections)
	}
	return nil
}

func machineStart(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn machine start NAME|SLUG|mch_… [--env K=V]… [--webhook URL] [--ref REF]")
	}
	fs := flag.NewFlagSet("machine start", flag.ContinueOnError)
	webhook := fs.String("webhook", "", "https URL for this wake's run events")
	ref := fs.String("ref", "", "external reference for this wake")
	var env envFlags
	fs.Var(&env, "env", "ephemeral environment variable NAME=value for this wake (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("usage: jinn machine start NAME|SLUG|mch_… [--env K=V]… [--webhook URL] [--ref REF]")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.StartMachineWith(ctx, args[0], jinn.MachineStartRequest{
		Environment:       env,
		Webhook:           *webhook,
		ExternalReference: *ref,
	})
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("%s %s %s\n", paint(cyan, m.Slug), m.State, paint(dim, m.ActiveRun))
	return nil
}

func machineHeartbeat(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn machine heartbeat NAME|SLUG|mch_… [--lease ID] [--ttl SECONDS] [--release]")
	}
	fs := flag.NewFlagSet("machine heartbeat", flag.ContinueOnError)
	lease := fs.String("lease", "", "activity lease id (default \"default\")")
	ttl := fs.Int("ttl", 0, "lease TTL in seconds (30 to 900, default 300)")
	release := fs.Bool("release", false, "release the lease instead of extending it")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("usage: jinn machine heartbeat NAME|SLUG|mch_… [--lease ID] [--ttl SECONDS] [--release]")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.HeartbeatMachine(ctx, args[0], jinn.MachineHeartbeatRequest{
		LeaseID:    *lease,
		TTLSeconds: *ttl,
		Release:    *release,
	})
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("%s %s leases=%d\n", paint(cyan, m.Slug), m.State, m.ActiveConnections)
	return nil
}

func machineStop(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn machine stop NAME|SLUG|mch_…")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.StopMachine(ctx, args[0])
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("%s %s\n", paint(cyan, m.Slug), m.State)
	return nil
}

func machineResize(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn machine resize NAME|SLUG|mch_… [--size s|m|l|xl] [--idle N] [--name NAME] [--timeout M] [--webhook URL] [--ref REF]")
	}
	fs := flag.NewFlagSet("machine resize", flag.ContinueOnError)
	size := fs.String("size", "", "new size: s, m, l or xl")
	idle := fs.Int("idle", 0, "new idle stop timeout in minutes")
	rename := fs.String("name", "", "new name")
	timeout := fs.Int("timeout", -1, "new wake timeout in minutes (0 clears)")
	webhook := fs.String("webhook", "", "new webhook https URL")
	ref := fs.String("ref", "", "new external reference")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	var req jinn.MachineUpdateRequest
	if *size != "" {
		req.Size = size
	}
	if *idle > 0 {
		req.IdleMinutes = idle
	}
	if *rename != "" {
		req.Name = rename
	}
	if *timeout >= 0 {
		req.TimeoutMinutes = timeout
	}
	if *webhook != "" {
		req.Webhook = webhook
	}
	if *ref != "" {
		req.ExternalReference = ref
	}
	if req.Size == nil && req.IdleMinutes == nil && req.Name == nil && req.TimeoutMinutes == nil && req.Webhook == nil && req.ExternalReference == nil {
		return usageError("pass --size, --idle, --name, --timeout, --webhook or --ref")
	}
	c, err := client()
	if err != nil {
		return err
	}
	m, err := c.UpdateMachine(ctx, args[0], req)
	if err != nil {
		return err
	}
	if asJSON {
		emit(m)
		return nil
	}
	fmt.Printf("%s %s size=%s idle=%dm\n", paint(cyan, m.Slug), m.State, m.Size, m.IdleMinutes)
	return nil
}

func machineSnapshot(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn machine snapshot NAME|SLUG|mch_…")
	}
	c, err := client()
	if err != nil {
		return err
	}
	img, err := c.SnapshotMachine(ctx, args[0])
	if err != nil {
		return err
	}
	if asJSON {
		emit(img)
		return nil
	}
	fmt.Printf("%s %s (%s)\n", paint(green, "→"), paint(cyan, img.ID), size(img.Bytes))
	return nil
}

func machineRm(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn machine rm NAME|SLUG|mch_…")
	}
	c, err := client()
	if err != nil {
		return err
	}
	if err := c.DeleteMachine(ctx, args[0]); err != nil {
		return err
	}
	if asJSON {
		emit(map[string]string{"deleted": args[0]})
	}
	return nil
}

func sshConnect(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn ssh NAME|SLUG|mch_…|run_… [--user USER] [-- SSH_ARGS…]")
	}
	targetRef := args[0]
	fs := flag.NewFlagSet("ssh", flag.ContinueOnError)
	user := fs.String("user", "", "Linux user inside the VM (default root)")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	var dest string
	if jinnRun(targetRef) {
		dest = targetRef + "@ssh.usejinn.com"
	} else {
		c, err := client()
		if err != nil {
			return err
		}
		m, err := c.Machine(ctx, targetRef)
		if err != nil {
			return err
		}
		dest = m.SSH
		if dest == "" {
			dest = m.Slug + "@ssh.usejinn.com"
		}
	}
	if *user != "" && *user != "root" {
		dest = *user + "+" + dest
	}
	sshArgs := append([]string{dest}, fs.Args()...)
	cmd := exec.CommandContext(ctx, "ssh", sshArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitStatus(ee.ExitCode())
		}
		return err
	}
	return nil
}
