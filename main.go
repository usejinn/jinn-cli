// Command jinn is Jinn's CLI (go install usejinn.com/jinn@latest). It uses
// the same API as everything else, through the Go SDK (usejinn.com/go).
//
//	jinn login                                   sign this machine in (a person approves it in the console)
//	jinn bases                                   list the bases a function can boot
//	jinn functions                               list the account's functions
//	jinn publish function.json                   make a function, or publish its next version
//	jinn run NAME|fnc_… -p PROMPT [--in DIR] [--out DIR] [--version N] [--webhook URL] [--ref REF] [--detach]
//	jinn runs [--function NAME|fnc_…] [--state active|succeeded|failed]
//	jinn show run_…                              print a run as JSON
//	jinn logs run_… [--follow]                   print a run's log
//	jinn output run_… --out DIR                  download a run's output folder
//	jinn providers                               list the account's providers
//	jinn models VENDOR                           list the models a vendor key can use (key on stdin)
//	jinn provider NAME|prv_… --vendor V --model M [--effort E] [--compact N] [--max-output N]
//	                                             make a provider, or publish its next version (key on stdin)
//
// The key comes from JINN_KEY, else ~/.config/jinn/key (jinn login writes it).
// JINN_API changes the API's address.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	jinn "usejinn.com/go"
)

const usage = `jinn: run agent work as a call.

  jinn login
  jinn bases
  jinn functions
  jinn publish function.json
  jinn run NAME|fnc_… --prompt PROMPT [--in DIR] [--out DIR] [--version N] [--webhook URL] [--ref REF] [--detach]
  jinn runs [--function NAME|fnc_…] [--state active|succeeded|failed]
  jinn show run_…
  jinn logs run_… [--follow]
  jinn output run_… --out DIR
  jinn providers
  jinn models openai|anthropic|xai                 < vendor-key
  jinn provider NAME|prv_… --vendor V --model M [--effort E] [--compact N] [--max-output N]   < vendor-key

Vendor keys are read from stdin, never from a flag: echo "$OPENAI_API_KEY" | jinn models openai
--json (any command): print JSON, one document or one line per event.
Exit status: 0 done, 1 the run failed, 2 usage, 3 the API refused, 4 anything else.
The key comes from JINN_KEY, else ~/.config/jinn/key. Docs: https://docs.usejinn.com/cli
`

// asJSON is --json: every command prints JSON on stdout (a document, or
// JSON lines for a stream) and errors as JSON on stderr.
var asJSON bool

// emit prints one JSON document as one line.
func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// The exit status says what kind of failure it was, so scripts and agents can
// branch on it without reading the message.
const (
	exitRunFailed = 1
	exitUsage     = 2
	exitRefused   = 3
	exitOther     = 4
)

// usageError is a command used wrongly.
type usageError string

func (e usageError) Error() string { return string(e) }

// runFailed is a run that ended in failed.
type runFailed struct{ run jinn.Run }

func (e runFailed) Error() string {
	return fmt.Sprintf("%s failed: %s: %s", e.run.ID, e.run.Failure, e.run.Detail)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var argv []string
	for _, a := range os.Args[1:] {
		if a == "--json" {
			asJSON = true
			continue
		}
		argv = append(argv, a)
	}
	if len(argv) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}
	cmd, args := argv[0], argv[1:]
	var err error
	switch cmd {
	case "login":
		err = login(ctx)
	case "bases":
		err = bases(ctx)
	case "functions":
		err = functions(ctx)
	case "publish":
		err = publish(ctx, args)
	case "run":
		err = run(ctx, args)
	case "runs":
		err = runs(ctx, args)
	case "show":
		err = show(ctx, args)
	case "logs":
		err = logs(ctx, args)
	case "output":
		err = output(ctx, args)
	case "providers":
		err = providers(ctx)
	case "models":
		err = models(ctx, args)
	case "provider":
		err = provider(ctx, args)
	case "version", "--version":
		v := "(built from source)"
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			v = bi.Main.Version
		}
		if asJSON {
			emit(map[string]string{"version": v})
		} else {
			fmt.Println("jinn", v)
		}
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = usageError(fmt.Sprintf("no command %q; run jinn help", cmd))
	}
	if err != nil {
		code, status, apiCode := exitOther, 0, ""
		var ue usageError
		var rf runFailed
		var ae *jinn.Error
		switch {
		case errors.As(err, &ue):
			code = exitUsage
		case errors.As(err, &rf):
			code = exitRunFailed
		case errors.As(err, &ae):
			code, status, apiCode = exitRefused, ae.Status, ae.Code
		}
		if asJSON {
			b, _ := json.Marshal(map[string]any{"error": err.Error(), "exit": code, "status": status, "code": apiCode})
			fmt.Fprintln(os.Stderr, string(b))
		} else {
			fmt.Fprintln(os.Stderr, "jinn:", strings.TrimPrefix(err.Error(), "jinn: "))
		}
		os.Exit(code)
	}
}

func apiURL() string {
	if u := os.Getenv("JINN_API"); u != "" {
		return u
	}
	return jinn.DefaultBaseURL
}

func keyFile() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "jinn", "key")
}

func client() (*jinn.Client, error) {
	key := os.Getenv("JINN_KEY")
	if key == "" {
		b, err := os.ReadFile(keyFile())
		if err != nil {
			return nil, errors.New("no key: run jinn login, or set JINN_KEY")
		}
		key = strings.TrimSpace(string(b))
	}
	c := jinn.New(key)
	c.BaseURL = apiURL()
	return c, nil
}

func login(ctx context.Context) error {
	host, _ := os.Hostname()
	name := "cli " + strings.Map(func(r rune) rune {
		if strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ._@:/-", r) {
			return r
		}
		return '-'
	}, host)
	if len(name) > 64 {
		name = name[:64]
	}
	l, err := jinn.StartLogin(ctx, apiURL(), name)
	if err != nil {
		return err
	}
	if asJSON {
		emit(map[string]any{"type": "login", "url": l.URL, "code": l.Code, "expires_at": l.ExpiresAt})
	} else {
		fmt.Printf("Open %s\nCheck that it shows %s, then approve.\n", l.URL, l.Code)
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(l.ExpiresAt, 0))
	defer cancel()
	key, err := l.Wait(ctx, apiURL())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile()), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile(), []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	if asJSON {
		emit(map[string]any{"type": "signed_in", "key_file": keyFile()})
	} else {
		fmt.Println("Signed in. The key is in", keyFile())
	}
	return nil
}

func bases(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.Bases(ctx)
	if err == nil && asJSON {
		emit(list)
		return nil
	}
	for _, b := range list {
		fmt.Printf("%s  %s\n", b.Name, size(b.Bytes))
	}
	return err
}

func functions(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.Functions(ctx)
	if err == nil && asJSON {
		emit(list)
		return nil
	}
	for _, f := range list {
		last := "never run"
		if f.Last != nil {
			last = "last run " + f.Last.State + " " + f.Last.CreatedAt.Format(time.DateTime)
		}
		fmt.Printf("%-28s %-30s v%-4d %s\n", f.ID, f.Name, f.Latest, last)
	}
	return err
}

// functionID resolves a name to the account's one function of that name.
func functionID(ctx context.Context, c *jinn.Client, nameOrID string) (string, error) {
	if strings.HasPrefix(nameOrID, "fnc_") {
		return nameOrID, nil
	}
	list, err := c.Functions(ctx)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, f := range list {
		if f.Name == nameOrID {
			ids = append(ids, f.ID)
		}
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no function is named %s", nameOrID)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%d functions are named %s; use an id: %s", len(ids), nameOrID, strings.Join(ids, ", "))
}

// publish reads {"name": …, …definition} and publishes the next version of
// the account's function with that name, or makes it.
func publish(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn publish function.json")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
		jinn.Definition
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	c, err := client()
	if err != nil {
		return err
	}
	id, err := functionID(ctx, c, in.Name)
	var v jinn.Version
	switch {
	case err == nil:
		v, err = c.Publish(ctx, id, in.Definition)
	case strings.HasPrefix(err.Error(), "no function"):
		v, err = c.CreateFunction(ctx, in.Name, in.Definition)
	}
	if err != nil {
		return err
	}
	if asJSON {
		emit(v)
		return nil
	}
	fmt.Printf("%s %s %s v%d\n", paint(green, "→"), in.Name, paint(cyan, v.Function), v.Version)
	return nil
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn run NAME|fnc_… --prompt PROMPT [--in DIR] [--out DIR]")
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	prompt := fs.String("prompt", "", "the agent's first message")
	fs.StringVar(prompt, "p", "", "the agent's first message")
	in := fs.String("in", "", "the input folder")
	out := fs.String("out", "", "where to put the output folder")
	version := fs.Int("version", 0, "the version (default: the latest)")
	webhook := fs.String("webhook", "", "where to send the result")
	ref := fs.String("ref", "", "your id for this run")
	detach := fs.Bool("detach", false, "print the run's id and return")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	c, err := client()
	if err != nil {
		return err
	}
	fnc, err := functionID(ctx, c, args[0])
	if err != nil {
		return err
	}
	req := jinn.RunRequest{Prompt: *prompt, Version: *version, Webhook: *webhook, ExternalReference: *ref}
	if *in != "" {
		if req.Input, err = c.UploadFolder(ctx, *in); err != nil {
			return err
		}
	}
	r, err := c.StartRun(ctx, fnc, req)
	if err != nil {
		return err
	}
	switch {
	case asJSON && *detach:
		emit(r)
		return nil
	case asJSON:
		emit(map[string]any{"type": "run", "run": r})
	default:
		fmt.Printf("%s queued %s\n", paint(cyan, r.ID), paint(dim, fmt.Sprintf("· %s v%d", args[0], r.Version)))
	}
	if *detach {
		return nil
	}
	r, err = follow(ctx, c, r)
	if err != nil {
		return err
	}
	if r.State != jinn.Succeeded {
		return runFailed{r}
	}
	if !asJSON {
		fmt.Printf("%s in %s\n", paint(green, "✓ succeeded"), took(r))
	}
	if *out != "" {
		return download(ctx, c, r, *out)
	}
	return nil
}

// download unpacks a run's output into dir and lists its files.
func download(ctx context.Context, c *jinn.Client, r jinn.Run, dir string) error {
	if err := c.DownloadOutput(ctx, r, dir); err != nil {
		return err
	}
	if asJSON {
		emit(map[string]any{"type": "output", "dir": dir, "files": r.Output.Files, "file_count": r.Output.FileCount})
		return nil
	}
	for _, f := range r.Output.Files {
		if !f.Dir {
			fmt.Printf("%s %s\n", filepath.Join(dir, f.Path), size(f.Bytes))
		}
	}
	return nil
}

// follow prints a run's log as it grows and returns the run once it ends.
// With --json it prints JSON lines: {"type":"run","run":…} when the run
// starts and when it ends, and {"type":"log",…} for each log event.
func follow(ctx context.Context, c *jinn.Client, r jinn.Run) (jinn.Run, error) {
	seen, running := 0, false
	for {
		var err error
		if r, err = c.Run(ctx, r.ID); err != nil {
			return r, err
		}
		if r.State != jinn.Queued && !running {
			running = true
			if asJSON {
				emit(map[string]any{"type": "run", "run": r})
			} else {
				fmt.Printf("%s running\n", paint(cyan, r.ID))
			}
		}
		if running {
			events, err := c.Log(ctx, r.ID)
			if err != nil {
				return r, err
			}
			for _, e := range events[min(seen, len(events)):] {
				if asJSON {
					emit(logLine(e))
				} else {
					printEvent(e)
				}
			}
			seen = len(events)
		}
		if r.Done() {
			if asJSON {
				emit(map[string]any{"type": "run", "run": r})
			}
			return r, nil
		}
		select {
		case <-ctx.Done():
			return r, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// logLine is a log event as one JSON object: type log, at, kind and its fields.
func logLine(e jinn.LogEvent) map[string]any {
	line := map[string]any{"type": "log", "at": e.At, "kind": e.Kind}
	for k, v := range e.Fields {
		line[k] = v
	}
	return line
}

// printEvent prints what the agent did, one line each.
func printEvent(e jinn.LogEvent) {
	s := func(k string) string { v, _ := e.Fields[k].(string); return v }
	line := func(v string) string {
		v = strings.Join(strings.Fields(v), " ")
		if len(v) > 160 {
			v = v[:157] + "…"
		}
		return v
	}
	switch e.Kind {
	case "setup":
		code, _ := e.Fields["exit_code"].(float64)
		fmt.Printf("%s  %s  %s\n", paint(dim, "setup"), paint(dim, line(s("command"))), paint(dim, fmt.Sprintf("→ %d", int(code))))
	case "text":
		fmt.Printf("%s  %s\n", paint(violet, "agent"), line(s("text")))
	case "tool_call":
		fmt.Printf("%s  %s %s\n", paint(violet, "agent"), s("name"), paint(dim, line(s("arguments"))))
	case "tool_output":
		if failed, _ := e.Fields["error"].(bool); failed {
			fmt.Printf("       %s → %s\n", s("name"), paint(red, line(s("output"))))
		}
	case "retry":
		fmt.Printf("%s  %s\n", paint(amber, "retry"), line(s("error")))
	}
}

func runs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	function := fs.String("function", "", "one function's runs")
	state := fs.String("state", "", "active, succeeded or failed")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	c, err := client()
	if err != nil {
		return err
	}
	q := jinn.RunsQuery{State: *state}
	if *function != "" {
		if q.Function, err = functionID(ctx, c, *function); err != nil {
			return err
		}
	}
	list, next, err := c.Runs(ctx, q)
	if err != nil {
		return err
	}
	if asJSON {
		emit(map[string]any{"runs": list, "next": next})
		return nil
	}
	fns, err := c.Functions(ctx)
	if err != nil {
		return err
	}
	names, width := map[string]string{}, 8
	for _, f := range fns {
		names[f.ID] = f.Name
	}
	for _, r := range list {
		width = max(width, len(names[r.Function]))
	}
	stateColour := map[string]string{jinn.Succeeded: green, jinn.Failed: red, jinn.Running: cyan, jinn.Queued: dim}
	for _, r := range list {
		name := names[r.Function]
		if name == "" {
			name = r.Function
		}
		fmt.Printf("%s  %-*s v%-3d %s %s  %-7s %s\n", paint(cyan, r.ID), width, name, r.Version,
			paint(stateColour[r.State], fmt.Sprintf("%-9s", r.State)), r.CreatedAt.Local().Format("2006-01-02 15:04"), took(r), r.Failure)
	}
	return nil
}

func show(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn show run_…")
	}
	c, err := client()
	if err != nil {
		return err
	}
	r, err := c.Run(ctx, args[0])
	if err != nil {
		return err
	}
	if asJSON {
		emit(r)
		return nil
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	fmt.Println(string(b))
	return nil
}

func logs(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: jinn logs run_… [--follow]")
	}
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	followFlag := fs.Bool("follow", false, "keep printing until the run ends")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	c, err := client()
	if err != nil {
		return err
	}
	r, err := c.Run(ctx, args[0])
	if err != nil {
		return err
	}
	if *followFlag {
		r, err = follow(ctx, c, r)
		if err == nil && !asJSON {
			fmt.Printf("%s %s %s\n", r.ID, r.State, r.Failure)
		}
		return err
	}
	events, err := c.Log(ctx, r.ID)
	if asJSON {
		for _, e := range events {
			emit(logLine(e))
		}
		return err
	}
	for _, e := range events {
		b, _ := json.Marshal(e.Fields)
		fmt.Printf("%s %-11s %s\n", e.At.Format("15:04:05"), e.Kind, b)
	}
	return err
}

func output(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: jinn output run_… --out DIR")
	}
	fs := flag.NewFlagSet("output", flag.ContinueOnError)
	out := fs.String("out", ".", "where to put the output folder")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	c, err := client()
	if err != nil {
		return err
	}
	r, err := c.Run(ctx, args[0])
	if err != nil {
		return err
	}
	if r.Output == nil {
		return fmt.Errorf("%s has no output: it is %s", r.ID, r.State)
	}
	return download(ctx, c, r, *out)
}

func providers(ctx context.Context) error {
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.Providers(ctx)
	if err == nil && asJSON {
		emit(list)
		return nil
	}
	for _, p := range list {
		v := p.Versions[0]
		effort := v.Model.ReasoningEffort
		if effort == "" {
			effort = "-"
		}
		fmt.Printf("%-28s %-24s v%-3d %-9s %-28s %s\n", p.ID, p.Name, p.Latest, v.Model.Provider, v.Model.Model, effort)
	}
	return err
}

// vendorKey reads a vendor's API key from stdin, so it never sits in a
// flag, the shell's history or the process list.
func vendorKey() (string, error) {
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprint(os.Stderr, "Paste the vendor's API key, then press Enter: ")
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 8192))
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	if key == "" {
		return "", errors.New("no key on stdin: echo \"$KEY\" | jinn …")
	}
	return key, nil
}

func models(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("usage: jinn models openai|anthropic|xai < vendor-key")
	}
	key, err := vendorKey()
	if err != nil {
		return err
	}
	c, err := client()
	if err != nil {
		return err
	}
	list, err := c.Catalog(ctx, args[0], key)
	if err == nil && asJSON {
		emit(list)
		return nil
	}
	for _, m := range list {
		efforts := strings.Join(m.Efforts, ",")
		if m.DefaultEffort != "" {
			efforts += " (default " + m.DefaultEffort + ")"
		}
		fmt.Printf("%-36s %s\n", m.ID, efforts)
	}
	return err
}

// providerID resolves a name to the account's one provider of that name.
func providerID(ctx context.Context, c *jinn.Client, nameOrID string) (string, error) {
	if strings.HasPrefix(nameOrID, "prv_") {
		return nameOrID, nil
	}
	list, err := c.Providers(ctx)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, p := range list {
		if p.Name == nameOrID {
			ids = append(ids, p.ID)
		}
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("no provider is named %s", nameOrID)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%d providers are named %s; use an id: %s", len(ids), nameOrID, strings.Join(ids, ", "))
}

// provider makes a provider, or publishes the next version of the one
// named: a new key, a new model or both. Jinn checks the key and the model
// against the vendor's catalog.
func provider(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("usage: jinn provider NAME|prv_… --vendor V --model M [--effort E] [--compact N] [--max-output N] < vendor-key")
	}
	fs := flag.NewFlagSet("provider", flag.ContinueOnError)
	vendor := fs.String("vendor", "", "openai, anthropic or xai")
	model := fs.String("model", "", "the model's id (jinn models VENDOR lists them)")
	effort := fs.String("effort", "", "the reasoning effort, if the model takes one")
	compact := fs.Int("compact", 200000, "the history size, in tokens, at which a run compacts")
	maxOutput := fs.Int("max-output", 0, "the most tokens per reply (0: the model's maximum)")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError(err.Error())
	}
	if *vendor == "" || *model == "" {
		return usageError("name the --vendor and the --model")
	}
	key, err := vendorKey()
	if err != nil {
		return err
	}
	c, err := client()
	if err != nil {
		return err
	}
	m := jinn.Model{Provider: *vendor, Model: *model, ReasoningEffort: *effort, CompactAtTokens: *compact, MaxOutputTokens: *maxOutput}
	id, err := providerID(ctx, c, args[0])
	var p jinn.ProviderVersion
	switch {
	case err == nil:
		p, err = c.PublishProvider(ctx, id, m, key)
	case strings.HasPrefix(err.Error(), "no provider"):
		p, err = c.CreateProvider(ctx, args[0], m, key)
	}
	if err != nil {
		return err
	}
	if asJSON {
		emit(p)
		return nil
	}
	fmt.Printf("%s %s %s v%d · use %s@latest in a function's provider\n", paint(green, "→"), p.Name, paint(cyan, p.ID), p.Version, p.ID)
	return nil
}

// Colours, only on a terminal and never with NO_COLOR set.
const (
	cyan   = "38;2;92;240;255"
	violet = "38;2;139;92;255"
	green  = "38;2;184;255;92"
	red    = "38;2;255;92;138"
	amber  = "38;2;255;181;71"
	dim    = "2"
)

var colour = func() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}()

func paint(code, s string) string {
	if !colour {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func took(r jinn.Run) string {
	if r.StartedAt == nil || r.EndedAt == nil {
		return ""
	}
	d := r.EndedAt.Sub(*r.StartedAt)
	if d >= time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

func size(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1f KB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d B", n)
}
