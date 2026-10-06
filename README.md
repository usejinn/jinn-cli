<p align="center"><img src="assets/run.gif" alt="jinn run answering a support ticket: setup, the agent's tool calls, the result folder" width="820"></p>

# jinn

Give an agent a task and a folder. Get files back.

`jinn` runs a [Jinn](https://usejinn.com) function: an AI agent with the tools, packages and model you chose, working in a fresh Linux VM until the files you asked for exist. You watch each step in your terminal, and the result lands in a folder.

```sh
curl -fsSL https://github.com/usejinn/jinn-cli/releases/latest/download/install.sh | sh
```

Or, with Go 1.25+: `go install usejinn.com/jinn@latest`. Builds for Linux, macOS and Windows are on the [releases page](https://github.com/usejinn/jinn-cli/releases).

## Start

```sh
jinn login                                   # approve this machine in the console
echo "$OPENAI_API_KEY" | jinn provider openai --vendor openai --model gpt-5.2
jinn publish function.json                   # what the agent may use, and what it must return
jinn run summarise --prompt "Summarise these notes." --in ./notes --out ./result
```

A function is one JSON file: the base and setup commands, the model, the tools, and the files that go in and must come out. The [quickstart](https://docs.usejinn.com/quickstart) has a complete one.

## The output is checked

The function names the files a run must return. The agent cannot finish until they exist and fit their size limits. When something is missing, it is told what and carries on.

<img src="assets/review.gif" alt="A pull request review where submit_result is refused until comments.json exists" width="820">

## Define once, run from anywhere

Each publish is a new version, and every run records the version it used. Start runs from a terminal, a cron job, CI or a webhook handler. `--detach` returns the run's id at once.

<img src="assets/publish.gif" alt="jinn publish, a detached run and the function's run history" width="820">

## Commands

| Command | Does |
|---|---|
| `jinn run FUNCTION --prompt TEXT [--in DIR] [--out DIR]` | Start a run, follow it, download its output |
| `jinn runs`, `jinn show RUN`, `jinn logs RUN --follow` | Read runs and their logs |
| `jinn publish FILE` | Make a function, or publish its next version |
| `jinn providers`, `jinn models VENDOR`, `jinn provider NAME` | Manage model keys (read from stdin) |
| `jinn login`, `jinn bases`, `jinn functions`, `jinn version` | Everything else |

Add `--json` to any command for JSON output, one document or one line per event. Exit statuses tell failures apart: 1 the run failed, 2 wrong usage, 3 the API refused, 4 anything else. See [Use Jinn from an agent](https://docs.usejinn.com/agents).

Full reference: [docs.usejinn.com/cli](https://docs.usejinn.com/cli). Jinn is invitation-only for now: write to [support@usejinn.com](mailto:support@usejinn.com).

## Verify

Releases are built only by [this repository's release workflow](.github/workflows/release.yml) and are immutable once published. Each file has a build provenance attestation:

```sh
gh attestation verify jinn-linux-amd64 --repo usejinn/jinn-cli
```

The build is reproducible. Rebuild a tag and compare its SHA-256 with the release's `SHA256SUMS`:

```sh
git checkout v0.4.0
GOTOOLCHAIN=go1.25.12 CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -buildid=" -o jinn-linux-amd64 .
sha256sum jinn-linux-amd64
```

---

Built on the [Go SDK](https://github.com/usejinn/jinn-go). MIT licence. This repository is published from Jinn's main source; open issues here.
