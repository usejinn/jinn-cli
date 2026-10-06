#!/usr/bin/env python3
"""Writes the README's terminal recordings as asciinema casts. Every line is
what the CLI prints, in its colours (cli/main.go); make.sh renders the GIFs.
The runs are written out, not recorded: a demo needs a model key and takes
minutes."""
import json

CYAN, VIOLET, GREEN, RED, DIM = "38;2;92;240;255", "38;2;139;92;255", "38;2;184;255;92", "38;2;255;92;138", "2"
def p(code, s): return f"\x1b[{code}m{s}\x1b[0m"
PROMPT = p(VIOLET, "❯") + " "

class Cast:
    def __init__(self, rows):
        self.t, self.events, self.rows = 0.4, [], rows
    def out(self, s, wait=0.0):
        self.t += wait
        self.events.append([round(self.t, 3), "o", s])
    def type(self, cmd, then=0.5):
        self.out(PROMPT, 0.2)
        for ch in cmd:
            self.out(ch, 0.035)
        self.out("\r\n", 0.35 + then)
    def line(self, s, wait=0.35):
        self.out(s + "\r\n", wait)
    def write(self, path, end=3.0):
        self.out("", end)
        with open(path, "w") as f:
            f.write(json.dumps({"version": 2, "width": 98, "height": self.rows}) + "\n")
            for e in self.events:
                f.write(json.dumps(e) + "\n")

def agent(s): return p(VIOLET, "agent") + "  " + s
def tool(name, args): return agent(name + " " + p(DIM, args))
def setup(cmd, code=0): return p(DIM, "setup") + "  " + p(DIM, cmd) + "  " + p(DIM, f"→ {code}")

# 1. A support ticket in, a reply and refunds out.
run = "run_7c41e0a9d2b35f8e61c09a4d"
c = Cast(19)
c.type('jinn run support-triage --prompt "Ticket #4812: charged twice" --in ./ticket --out ./result')
c.line(p(CYAN, run) + " queued " + p(DIM, "· support-triage v14"), 0.6)
c.line(p(CYAN, run) + " running", 2.2)
c.line(setup("apt-get install -y --no-install-recommends poppler-utils"), 1.4)
c.line(tool("read", '{"path":"/workspace/in/ticket.json"}'), 1.2)
c.line(tool("lookup_customer", '{"email":"maya@northwind.example"}'), 1.0)
c.line(tool("bash", '{"command":"jq \'.charges[] | select(.amount == 2900)\' charges.json"}'), 1.2)
c.line(agent("Two charges for £29.00, 2 seconds apart. A double submit; refund the second."), 1.4)
c.line(tool("write", '{"path":"/workspace/out/reply.md"}'), 1.0)
c.line(tool("write", '{"path":"/workspace/out/actions.json"}'), 0.8)
c.line(tool("submit_result", "{}"), 0.9)
c.line(p(GREEN, "✓ succeeded") + " in 38.4s", 1.0)
c.line("result/reply.md 1.2 KB", 0.3)
c.line("result/actions.json 86 B", 0.2)
c.type("cat result/actions.json", 0.2)
c.line('[{"charge": "ch_3Q8kd2", "amount": 2900, "reason": "duplicate"}]', 0.1)
c.write("run.cast", 4.0)

# 2. The output manifest holds the agent to its job.
run = "run_b20f6d1e94a7c3580e2d7f16"
c = Cast(15)
c.type('jinn run pr-review --prompt "Review #318" --in ./pr --out ./review')
c.line(p(CYAN, run) + " queued " + p(DIM, "· pr-review v6"), 0.6)
c.line(p(CYAN, run) + " running", 1.0)
c.line(setup("apt-get install -y --no-install-recommends golang-go"), 1.6)
c.line(tool("bash", '{"command":"cd /workspace/in/repo && git apply ../change.diff && go test ./..."}'), 1.2)
c.line(agent("TestClaimExpired fails: the deadline compares local time with UTC."), 1.6)
c.line(tool("write", '{"path":"/workspace/out/review.md"}'), 1.0)
c.line(tool("submit_result", "{}"), 0.9)
c.line("       submit_result → " + p(RED, '{"accepted":false,"problems":["comments.json: missing"]}'), 0.7)
c.line(tool("write", '{"path":"/workspace/out/comments.json"}'), 1.4)
c.line(tool("submit_result", "{}"), 0.9)
c.line(p(GREEN, "✓ succeeded") + " in 4m12s", 1.0)
c.line("review/comments.json 2.1 KB", 0.3)
c.line("review/review.md 4.8 KB", 0.2)
c.write("review.cast", 4.0)

# 3. Define once; call it from anything.
c = Cast(11)
c.type("jinn publish function.json")
c.line(p(GREEN, "→") + " monday-report " + p(CYAN, "fnc_9a3e17c25b0d48f6e2a71c34") + " v3", 0.7)
c.type('jinn run monday-report --prompt "Week 40" --in ./exports --detach', 0.3)
c.line(p(CYAN, "run_e5c80a2f71d93b46a0c1e8d5") + " queued " + p(DIM, "· monday-report v3"), 0.6)
c.type("jinn runs --function monday-report", 0.3)
rows = [
    ("run_e5c80a2f71d93b46a0c1e8d5", "v3", CYAN, "running  ", "2026-10-05 09:00", ""),
    ("run_0b7d24e9c1a65f38d2e0b917", "v2", GREEN, "succeeded", "2026-09-28 09:00", "3m41s"),
    ("run_6f1a93c0e8d27b54a9c3f0e1", "v2", GREEN, "succeeded", "2026-09-21 09:00", "4m2s"),
    ("run_c2e7b05d91f84a36e0d1b7a9", "v1", RED, "failed   ", "2026-09-14 09:00", "15m0s", "timeout"),
]
for r in rows:
    c.line(f"{p(CYAN, r[0])}  monday-report v{r[1][1:]:<3} {p(r[2], r[3])} {r[4]}  {r[5]:<7} {r[6] if len(r) > 6 else ''}".rstrip(), 0.08)
c.write("publish.cast", 4.0)
