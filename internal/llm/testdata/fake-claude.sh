#!/bin/sh
# fake-claude.sh: $1 is -p, $2 is the prompt "/<skill> Input: <file>. ..."
skill=$(printf '%s' "$2" | sed -n 's|^/\([a-z-]*\) .*|\1|p')
in=$(printf '%s' "$2" | sed -n 's|.*Input: \([^ ]*\.json\).*|\1|p')
[ -f "$HOME/claude.lock" ] && echo overlap > "$HOME/claude.overlap"
touch "$HOME/claude.lock"
printf '%s\n' "$*" >> "$HOME/claude.args"
env | cut -d= -f1 | sort > "$HOME/claude.env"
cp "$in" "$HOME/claude.$skill.input"
if [ -f "$HOME/claude.sleep" ]; then
	sleep "$(cat "$HOME/claude.sleep")" &
	echo $! > "$HOME/claude.sleep.pid"
	wait $!
fi
rm -f "$HOME/claude.lock"
cat "$HOME/claude.$skill.stdout"
exit "$(cat "$HOME/claude.exit" 2>/dev/null || echo 0)"
