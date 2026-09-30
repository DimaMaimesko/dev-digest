#!/bin/sh
# Validates the INSIGHTS.md files against the engineering-insights entry format.
# Usage (from anywhere in the repo): .claude/skills/engineering-insights/check.sh [file...]
# With no arguments it checks the root, api/, client/ and e2e/ files.
# Exit status 1 if any entry is malformed.
set -eu

root=$(git rev-parse --show-toplevel)
cd "$root"
[ $# -gt 0 ] || set -- INSIGHTS.md api/INSIGHTS.md client/INSIGHTS.md e2e/INSIGHTS.md

status=0
for f in "$@"; do
  if [ ! -f "$f" ]; then
    echo "$f: missing"
    status=1
    continue
  fi
  awk -v file="$f" '
    function flush() {
      if (title == "") return
      miss = ""
      if (!cause) miss = miss " Cause:"
      if (!rule) miss = miss " Rule:"
      if (!promoted) miss = miss " Promoted:"
      if (kind == "mistake" && !symptom) miss = miss " Symptom:"
      if (miss != "") { printf "%s:%d: \"%s\" lacks%s\n", file, line, title, miss; bad++ }
    }
    # Entries start after the first line that is exactly "---" (the header holds a format sample).
    !body { if ($0 == "---") body = 1; next }
    /^## / {
      flush()
      n++; line = NR; title = substr($0, 4)
      cause = rule = promoted = symptom = 0; kind = ""
      if ($0 !~ /^## [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9] · (mistake|pattern|decision|context) · ./) {
        printf "%s:%d: bad heading, want \"## YYYY-MM-DD · mistake|pattern|decision|context · title\"\n", file, NR
        bad++; title = ""
        next
      }
      split($0, parts, " · "); kind = parts[2]
      next
    }
    /^Symptom:/  { symptom = 1 }
    /^Cause:/    { cause = 1 }
    /^Rule:/     { rule = 1 }
    /^Promoted:/ { promoted = 1; if ($0 ~ /^Promoted: no/) open++ }
    END {
      flush()
      if (!body) { printf "%s: no \"---\" line ending the header\n", file; bad++ }
      printf "%s: %d entries, %d not promoted%s\n", file, n, open, (n > 100 ? " (over 100: time to prune)" : "")
      exit bad > 0
    }
  ' "$f" || status=1
done
exit $status
