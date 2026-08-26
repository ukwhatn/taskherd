#!/bin/bash
# Record the screenshots in docs/assets from the fixtures in this directory.
#
#   scripts/demo/record.sh          # both languages
#   scripts/demo/record.sh en       # one of them
#
# Everything runs against a throwaway state directory seeded from tasks.json and cache.json, so no
# real task, repository or ticket can reach a published image. Nothing here touches the network:
# the config disables both the background fetch and the update check.
#
# Requires vhs (brew install vhs) and a Nerd Font, since the default icon set uses its glyphs.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out="$root/docs/assets"
work="$here/.work"
bin="$here/.work/bin"

command -v vhs >/dev/null 2>&1 || {
	echo "record.sh: vhs is not installed (brew install vhs)" >&2
	exit 1
}

langs=("$@")
if [ ${#langs[@]} -eq 0 ]; then
	langs=(ja en)
fi

mkdir -p "$out"

for lang in "${langs[@]}"; do
	echo "recording $lang"
	rm -rf "$work"
	mkdir -p "$work/state/taskherd" "$work/config" "$bin"

	go build -o "$bin/taskherd" "$root/cmd/taskherd"
	cp "$here/tasks.json" "$work/state/taskherd/tasks.json"
	cp "$here/cache.json" "$work/state/taskherd/cache.json"
	sed "s|^language = .*|language = \"$lang\"|" "$here/config.toml" >"$work/config/config.toml"

	sed -e "s|__WORK__|$work|g" \
		-e "s|__BIN__|$bin|g" \
		-e "s|__OUT__|$out|g" \
		-e "s|__LANG__|$lang|g" \
		"$here/board.tape.tmpl" >"$work/board.tape"

	vhs "$work/board.tape"
done

rm -rf "$work"
echo "wrote $out"
