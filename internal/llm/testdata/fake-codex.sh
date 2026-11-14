#!/bin/sh
# fake-codex.sh: the last argument is the prompt "Use the $<skill> skill. Input: <file>. ...";
# the argument after -o is the answer file
out=
prev=
for arg in "$@"; do
	[ "$prev" = "-o" ] && out=$arg
	prev=$arg
done
skill=$(printf '%s' "$prev" | sed -n 's|^Use the \$\([a-z-]*\) skill.*|\1|p')
in=$(printf '%s' "$prev" | sed -n 's|.*Input: \([^ ]*\.json\).*|\1|p')
[ -f "$HOME/codex.lock" ] && echo overlap > "$HOME/codex.overlap"
touch "$HOME/codex.lock"
printf '%s\n' "$*" >> "$HOME/codex.args"
env | cut -d= -f1 | sort > "$HOME/codex.env"
cp "$in" "$HOME/codex.$skill.input"
[ -f "$HOME/codex.sleep" ] && sleep "$(cat "$HOME/codex.sleep")"
rm -f "$HOME/codex.lock"
cp "$HOME/codex.$skill.output" "$out"
exit "$(cat "$HOME/codex.exit" 2>/dev/null || echo 0)"
