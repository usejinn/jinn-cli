<p align="center"><img src="assets/run.gif" alt="jinn run answering a support ticket: the agent's tool calls, then the result folder" width="820"></p>

# jinn

Give an agent a task and a folder. Get files back.

`jinn` runs a [Jinn](https://usejinn.com) function in a fresh Linux VM. With an agent, the model you chose works with the tools and packages you chose until the files you asked for exist; you watch each step in your terminal, and the result lands in a folder. Without one, the VM is yours: run commands in it, copy files in and out, and keep its disk as an image for the next run.

```sh
curl -fsSL https://github.com/usejinn/jinn-cli/releases/latest/download/install.sh | sh
```

Update it later with `jinn update`. Or, with Go 1.25+: `go install usejinn.com/jinn@latest`. Builds for Linux, macOS and Windows are on the [releases page](https://github.com/usejinn/jinn-cli/releases).

## Start

```sh
jinn login                                   # approve this machine in the console
echo "$OPENAI_API_KEY" | jinn provider openai --vendor openai --model gpt-5.2
jinn publish function.json                   # what the agent may use, and what it must return
jinn run summarise --prompt "Summarise these notes." --in ./notes --out ./result
```

A function is one JSON file: the image and setup commands, the agent (its model and tools), and the files that go in and must come out. The [quickstart](https://docs.usejinn.com/quickstart) has a complete one.

## The output is checked

The function names the files a run must return. A run cannot finish until they exist and fit their size limits. When something is missing, the agent is told what and carries on; `jinn finish` says what, and the run goes on.

<img src="assets/check.gif" alt="jinn finish refused because report.md is missing, then accepted once it exists" width="820">

## Define once, run from anywhere

Each publish is a new version, and every run records the version it used. Start runs from a terminal, a cron job, CI or a webhook handler. `--detach` returns the run's id at once.

<img src="assets/publish.gif" alt="jinn publish, a detached run and the function's run history" width="820">

## Drive it yourself

A function without an agent starts a VM and waits for you. `jinn exec` runs a command in it and exits with the command's status; `jinn cp` copies files in and out. `jinn finish` ends the run, and a function with `"output_image": true` keeps the disk as an image that the next run can boot.

<img src="assets/machine.gif" alt="jinn run of a function without an agent, jinn cp and jinn exec into it, jinn finish saving an image, and a run from that image" width="820">

## Commands

| Command | Does |
|---|---|
| `jinn run FUNCTION [--prompt TEXT] [--in DIR] [--out DIR] [--image IMG]` | Start a run; follow an agent's run and download its output |
| `jinn exec RUN -- COMMAND`, `jinn cp`, `jinn shell RUN`, `jinn finish RUN [--failed REASON]` | Act on a run: run a command, copy files, open a terminal, end it |
| `jinn runs`, `jinn show RUN`, `jinn logs RUN --follow` | Read runs and their logs |
| `jinn publish FILE`, `jinn function rm FUNCTION` | Make a function, or publish its next version; delete a function |
| `jinn images`, `jinn image rm IMG` | List the account's images and Jinn's bases; delete an image |
| `jinn providers`, `jinn models VENDOR`, `jinn provider NAME`, `jinn provider rm PROVIDER` | Manage model keys (read from stdin); delete a provider no function uses |
| `jinn update` | Replace `jinn` with the latest release, once its signature and checksum check out |
| `jinn login`, `jinn functions`, `jinn version` | Everything else |

Add `--json` to any command for JSON output, one document or one line per event. Exit statuses tell failures apart: 1 the run failed, 2 wrong usage, 3 the API refused, 4 anything else. `jinn exec` exits with the command's own status, and 125 when Jinn could not run it. See [Use Jinn from an agent](https://docs.usejinn.com/agents).

Full reference: [docs.usejinn.com/cli](https://docs.usejinn.com/cli). Sign up at [app.usejinn.com](https://app.usejinn.com).

## Verify

Releases are built only by [this repository's release workflow](.github/workflows/release.yml) and are immutable once published. Each file has a build provenance attestation:

```sh
gh attestation verify jinn-linux-amd64 --repo usejinn/jinn-cli
```

The release workflow also signs each release with Jinn's release key (Ed25519), over the line `jinn-cli VERSION` and `SHA256SUMS`, as `SHA256SUMS.sig`. `jinn update` checks that signature with [`release-key.pem`](release-key.pem), which the binary embeds. [docs.usejinn.com/cli#verify](https://docs.usejinn.com/cli#verify) shows how to check it with OpenSSL.

The build is reproducible. Rebuild a tag and compare its SHA-256 with the release's `SHA256SUMS`:

```sh
git checkout v0.6.1
GOTOOLCHAIN=go1.25.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -buildid=" -o jinn-linux-amd64 .
sha256sum jinn-linux-amd64
```

---

Built on the [Go SDK](https://github.com/usejinn/jinn-go). MIT licence. This repository is published from Jinn's main source; open issues here.
