#!/bin/sh
set -eu

REPO="uesteibar/ralph"
BINARY="ralph"
INSTALL_DIR="/usr/local/bin"

main() {
    os="$(detect_os)"
    arch="$(detect_arch)"
    tag="$(latest_tag)"
    asset="${BINARY}-${os}-${arch}"
    checksum_file="checksums.txt"

    url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
    checksum_url="https://github.com/${REPO}/releases/download/${tag}/${checksum_file}"

    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' EXIT

    echo "Installing ${BINARY} ${tag} (${os}/${arch})..."

    download "$url" "${tmpdir}/${asset}"
    download "$checksum_url" "${tmpdir}/${checksum_file}"

    verify_checksum "${tmpdir}" "${asset}" "${checksum_file}"

    install_binary "${tmpdir}/${asset}" "${INSTALL_DIR}/${BINARY}"

    echo "${BINARY} ${tag} installed to ${INSTALL_DIR}/${BINARY}"
    "${INSTALL_DIR}/${BINARY}" --version
}

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
    esac
}

latest_tag() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
            sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" |
            sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p'
    else
        echo "Error: curl or wget is required" >&2
        exit 1
    fi
}

download() {
    src="$1"
    dst="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dst" "$src"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dst" "$src"
    else
        echo "Error: curl or wget is required" >&2
        exit 1
    fi
}

verify_checksum() {
    dir="$1"
    file="$2"
    checksums="$3"

    expected="$(grep "${file}$" "${dir}/${checksums}" | awk '{print $1}')"
    if [ -z "$expected" ]; then
        echo "Error: checksum not found for ${file}" >&2
        exit 1
    fi

    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(cd "$dir" && sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(cd "$dir" && shasum -a 256 "$file" | awk '{print $1}')"
    else
        echo "Warning: no checksum tool found, skipping verification" >&2
        return 0
    fi

    if [ "$expected" != "$actual" ]; then
        echo "Error: checksum mismatch for ${file}" >&2
        echo "  expected: ${expected}" >&2
        echo "  actual:   ${actual}" >&2
        exit 1
    fi
}

install_binary() {
    src="$1"
    dst="$2"
    chmod +x "$src"
    if [ -w "$(dirname "$dst")" ]; then
        mv "$src" "$dst"
    else
        echo "Elevated permissions required to install to $(dirname "$dst")"
        sudo mv "$src" "$dst"
    fi
}

main
