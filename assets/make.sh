#!/usr/bin/env bash
# Renders the README's GIFs from casts.py with agg (github.com/asciinema/agg)
# in the website's palette, with JetBrains Mono (fonts-jetbrains-mono).
set -euo pipefail
cd "$(dirname "$0")"
python3 casts.py
theme=0b0b12,ecebf5,0b0b12,ff5c8a,b8ff5c,ffb547,8b5cff,8b5cff,5cf0ff,ecebf5,4a4960,ff5c8a,b8ff5c,ffb547,8b5cff,8b5cff,5cf0ff,ffffff
for name in run review publish; do
  agg --theme "$theme" --font-family "JetBrains Mono" --font-size 15 --line-height 1.45 --fps-cap 20 --idle-time-limit 3 "$name.cast" "$name.gif"
done
rm -f ./*.cast
ls -la ./*.gif
