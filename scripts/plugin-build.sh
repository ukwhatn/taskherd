#!/bin/sh
# Put a released taskherd binary at bin/taskherd, as the manifest's [[build]] step.
#
# herdr runs build commands during `plugin install` and lets them generate files, so installing the
# plugin does not have to compile anything: this downloads the release the manifest names. That is
# what keeps a Go toolchain off the requirement list for people who only use taskherd.
#
# Local development goes the other way round — build the tree yourself and `herdr plugin link`,
# which does not run build commands at all.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
manifest="${root}/herdr-plugin.toml"

die() {
	printf 'plugin-build.sh: %s\n' "$1" >&2
	exit 1
}

[ -f "$manifest" ] || die "no herdr-plugin.toml at ${manifest}"

# Reading, never writing: herdr aborts the install if the manifest changes after its preview.
version=$(sed -n 's/^version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)
[ -n "$version" ] || die "no version in ${manifest}"

TASKHERD_VERSION="v${version}" \
	TASKHERD_INSTALL_DIR="${root}/bin" \
	TASKHERD_NO_PATH_HINT=1 \
	sh "${root}/install.sh"

# The manifest picked which release to download, so a disagreement here means the two have drifted:
# a tag published without the manifest bump, or the bump landing before the release exists. Failing
# the build leaves the plugin unregistered, which beats registering one whose binary is not what the
# manifest says it is.
installed=$("${root}/bin/taskherd" --json version |
	sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
	head -n 1)
[ "$installed" = "$version" ] ||
	die "installed taskherd reports ${installed:-nothing}, but the manifest says ${version}"
