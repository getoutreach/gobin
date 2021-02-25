#!/usr/bin/env bash
#
# Run a golang binary using gobin

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
GOBINVERSION=v0.0.14
GOBINPATH="$DIR/../bin/gobin"
GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)

# shellcheck source=./lib/logging.sh
source "$DIR/lib/logging.sh"

PRINT_PATH=false
if [[ $1 == "-p" ]]; then
  PRINT_PATH=true
  shift
fi

if [[ -z $1 ]] || [[ $1 =~ ^(--help|-h) ]]; then
  echo "Usage: $0 [-p|-h|--help] <package> [args]" >&2
  exit 1
fi

if [[ ! -e $GOBINPATH ]]; then
  {
    mkdir -p "$(dirname "$GOBINPATH")"
    info "installing gobin@$GOBINVERSION into '$GOBINPATH'"
    curl --location --output "$GOBINPATH" --silent "https://github.com/myitcv/gobin/releases/download/$GOBINVERSION/$GOOS-$GOARCH"
    chmod +x "$GOBINPATH"
  } >&2
fi

# gobin picks up the Go binary from the path and runs it from within
# the temp directory while building things.  This has the unfortunate
# side effect that the version of Go used depends on what was used
# with `asdf global golang <ver>` command.  To make this
# deterministic, we create a temp directory and fill it in with the
# current .tool-versions file and then convince gobin to use it.
# shellcheck disable=SC2155
gobin_tmpdir="$(mktemp -d -t gobin-XXXXXXXX)"
trap 'rm -rf "$gobin_tmpdir"' EXIT INT TERM
cp "$DIR/../.tool-versions" "$gobin_tmpdir/.tool-versions"

# Change into the temporary directory
pushd "$gobin_tmpdir" >/dev/null || exit 1
BIN_PATH=$(/usr/bin/env bash -c "export TMPDIR='$gobin_tmpdir'; unset GOFLAGS; '$GOBINPATH' -p '$1'")
shift
popd >/dev/null || exit 1
rm -rf "$gobin_tmpdir"

if [[ $PRINT_PATH == "true" ]]; then
  echo "$BIN_PATH"
  exit
fi

exec "$BIN_PATH" "$@"
