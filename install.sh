#!/bin/sh
# Install taskherd from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/ukwhatn/taskherd/main/install.sh | sh
#
# Environment:
#   TASKHERD_VERSION      tag to install (default: the latest release)
#   TASKHERD_INSTALL_DIR  where to put the binary (default: $HOME/.local/bin)
#   TASKHERD_BASE_URL     release host, for testing against a local server
#   TASKHERD_NO_PATH_HINT set when another program drives this script and the PATH advice at the
#                         end would be aimed at a directory the user never types
#
# The default install directory is under $HOME on purpose: it needs no sudo, and `taskherd update`
# can replace a binary there without asking for any.

set -eu

REPO="ukwhatn/taskherd"
BASE_URL="${TASKHERD_BASE_URL:-https://github.com/${REPO}/releases/download}"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_DIR="${TASKHERD_INSTALL_DIR:-${HOME}/.local/bin}"

die() {
	printf 'install.sh: %s\n' "$1" >&2
	exit 1
}

note() {
	printf '%s\n' "$1" >&2
}

# fetch writes a URL to stdout using whichever downloader is present.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO- "$1"
	else
		die 'neither curl nor wget is available'
	fi
}

detect_platform() {
	os=$(uname -s)
	case "$os" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) die "unsupported OS: ${os} (taskherd builds for darwin and linux)" ;;
	esac

	arch=$(uname -m)
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "unsupported architecture: ${arch} (taskherd builds for amd64 and arm64)" ;;
	esac

	printf '%s_%s' "$os" "$arch"
}

latest_tag() {
	# The tag is the only field needed, and pulling it with sed keeps jq off the requirement list.
	fetch "$API_URL" |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
		head -n 1
}

# verify_checksum compares one file against its line in a checksums.txt, using whichever of the two
# usual tools this system has. It is skipped, loudly, when neither is present.
verify_checksum() {
	file=$1
	sums=$2
	name=$(basename "$file")

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$file" | cut -d' ' -f1)
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$file" | cut -d' ' -f1)
	else
		note 'warning: no sha256sum or shasum found; skipping checksum verification'
		return 0
	fi

	expected=$(grep " ${name}\$" "$sums" | cut -d' ' -f1 | head -n 1)
	[ -n "$expected" ] || die "no checksum for ${name} in checksums.txt"
	[ "$actual" = "$expected" ] || die "checksum mismatch for ${name}: got ${actual}, want ${expected}"
}

main() {
	platform=$(detect_platform)

	tag="${TASKHERD_VERSION:-}"
	if [ -z "$tag" ]; then
		tag=$(latest_tag)
		[ -n "$tag" ] || die "could not read the latest release tag from ${API_URL}"
	fi

	archive="taskherd_${platform}.tar.gz"
	tmp=$(mktemp -d)
	# The temporary directory goes whether the download succeeded or not.
	trap 'rm -rf "$tmp"' EXIT INT TERM

	note "downloading taskherd ${tag} (${platform})"
	fetch "${BASE_URL}/${tag}/${archive}" >"${tmp}/${archive}" ||
		die "could not download ${BASE_URL}/${tag}/${archive}"
	fetch "${BASE_URL}/${tag}/checksums.txt" >"${tmp}/checksums.txt" ||
		die "could not download the checksums for ${tag}"

	verify_checksum "${tmp}/${archive}" "${tmp}/checksums.txt"

	tar -xzf "${tmp}/${archive}" -C "$tmp" taskherd || die "could not unpack ${archive}"

	mkdir -p "$INSTALL_DIR" || die "could not create ${INSTALL_DIR}"
	chmod 0755 "${tmp}/taskherd"
	# Landing in the destination directory first keeps the move on one filesystem, so it replaces
	# any running copy atomically instead of truncating it halfway.
	mv "${tmp}/taskherd" "${INSTALL_DIR}/taskherd.new" || die "could not write to ${INSTALL_DIR}"
	mv "${INSTALL_DIR}/taskherd.new" "${INSTALL_DIR}/taskherd" || die "could not install into ${INSTALL_DIR}"

	note "installed ${INSTALL_DIR}/taskherd"

	if [ -n "${TASKHERD_NO_PATH_HINT:-}" ]; then
		return 0
	fi

	case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*)
		note ''
		note "${INSTALL_DIR} is not on your PATH. Add it:"
		note "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc   # bash"
		note "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc    # zsh"
		note "  fish_add_path ${INSTALL_DIR}                                # fish"
		;;
	esac
}

main "$@"
