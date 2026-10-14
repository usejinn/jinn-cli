#!/usr/bin/env python3
"""Writes the README's terminal recordings as asciinema casts; make.sh renders
the GIFs. Each cast replays sessions/NAME.txt: real runs, as the CLI printed
them (v0.5.2, 14 October 2026). A line starting "$ " is typed; every other
line is coloured as cli/main.go colours it. Only the pauses are made up, so a
demo does not take a run's real minutes."""
import json, re

CYAN, VIOLET, GREEN, RED, DIM = "38;2;92;240;255", "38;2;139;92;255", "38;2;184;255;92", "38;2;255;92;138", "2"
def p(code, s): return f"\x1b[{code}m{s}\x1b[0m"
PROMPT = p(VIOLET, "❯") + " "
STATE = {"succeeded": GREEN, "failed": RED, "running": CYAN, "queued": DIM}

def paint(line):
    """Colours one line of the CLI's output as the CLI does."""
    if m := re.fullmatch(r"(run_\w+) (queued) (· .*)", line):
        return p(CYAN, m[1]) + " queued " + p(DIM, m[3])
    if m := re.fullmatch(r"(run_\w+) running", line):
        return p(CYAN, m[1]) + " running"
    if m := re.fullmatch(r"(run_\w+) ready (· .*)", line):
        return p(GREEN, m[1]) + " ready " + p(DIM, m[2])
    if m := re.fullmatch(r"(run_\w+) (succeeded|failed)(.*)", line):
        return p(CYAN, m[1]) + " " + m[2] + m[3]
    if m := re.fullmatch(r"(run_\w+)(  \S+ +v\S+ +)(\w+ *)(.*)", line):
        return p(CYAN, m[1]) + m[2] + p(STATE[m[3].strip()], m[3]) + m[4]
    if m := re.fullmatch(r"agent  (\S+) (.*)", line):
        return p(VIOLET, "agent") + "  " + m[1] + " " + p(DIM, m[2])
    if m := re.fullmatch(r"( +\S+ → )(.*)", line):
        return m[1] + p(RED, m[2])
    if m := re.fullmatch(r"setup  (.*)", line):
        return p(DIM, "setup") + "  " + p(DIM, m[1])
    if m := re.fullmatch(r"✓ succeeded( in .*)", line):
        return p(GREEN, "✓ succeeded") + m[1]
    if m := re.fullmatch(r"→ (\S+) (\S+) (v\d+)", line):
        return p(GREEN, "→") + f" {m[1]} " + p(CYAN, m[2]) + f" {m[3]}"
    if m := re.fullmatch(r"image (img_\w+)", line):
        return "image " + p(CYAN, m[1])
    return line

class Cast:
    def __init__(self, rows):
        self.t, self.events, self.rows = 0.4, [], rows
    def out(self, s, wait=0.0):
        self.t += wait
        self.events.append([round(self.t, 3), "o", s])
    def type(self, cmd, then=0.5):
        self.out(PROMPT, 0.2)
        for ch in cmd:
            self.out(ch, 0.03)
        self.out("\r\n", 0.35 + then)
    def write(self, path, end=3.0):
        self.out("", end)
        with open(path, "w") as f:
            f.write(json.dumps({"version": 2, "width": 98, "height": self.rows}) + "\n")
            for e in self.events:
                f.write(json.dumps(e) + "\n")

def pause(line):
    """How long the replay waits before a line: a run's steps take a moment."""
    if re.fullmatch(r"run_\w+ (running|ready .*)", line) or line.startswith("image "):
        return 1.8
    if line.startswith(("agent  ", "jinn: ", "✓ ")) or re.fullmatch(r"run_\w+ (succeeded|failed).*", line):
        return 1.2
    return 0.25

# Each cast: its name, which the README's GIF is named after, and its height.
for name, rows in [("run", 26), ("check", 14), ("publish", 12), ("machine", 18)]:
    c = Cast(rows)
    with open(f"sessions/{name}.txt") as f:
        for line in f.read().rstrip("\n").split("\n"):
            if line.startswith("$ "):
                c.type(line[2:], 0.3)
            else:
                c.out(paint(line) + "\r\n", pause(line))
    c.write(f"{name}.cast", 4.0)
