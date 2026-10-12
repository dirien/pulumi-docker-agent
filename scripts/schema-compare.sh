#!/usr/bin/env bash
# Copyright 2026, Engin Diri.
# SPDX-License-Identifier: Apache-2.0
#
# Reports SDK-breaking changes between two versions of the Pulumi schema
# (provider/cmd/pulumi-resource-docker-agent/schema.json) with pulumi/schema-tools: removed or
# renamed resources, functions, types and properties, changed property types (a string that
# became an enum), properties that became required, and so on.
#
#   scripts/schema-compare.sh OLD_SCHEMA NEW_SCHEMA REPORT
#
# Writes the Markdown report of `schema-tools compare` to REPORT. When GITHUB_OUTPUT is set, it
# also writes `breaking=true` or `breaking=false` there. It exits 0 with or without breaking
# changes, and non-zero only when the comparison could not run.
#
# The pinned schema-tools release is downloaded into bin/ and checked against the SHA-256 sums
# below, unless SCHEMA_TOOLS names a schema-tools binary to use instead.
set -euo pipefail

SCHEMA_TOOLS_VERSION=v0.8.1

# The SHA-256 of the schema-tools release archive for a platform (checksums.txt of the release).
schema_tools_sha256() {
  case "$1" in
    linux-amd64) echo 8bd60e19dc61f8566738e9ad43e1c18dd0e8401d8de4a39258ac8fc38097be7e ;;
    linux-arm64) echo 8fba900d8e081d6fd4ca98198e6c903b6a59724f8aac69ddc59d54609768fb08 ;;
    darwin-amd64) echo 361c99a4977a6bf166104c8c92a6144fcd33b37a5707024394b71603e107f970 ;;
    darwin-arm64) echo 734ef910eda72558c3dd268bef385f220df916d443aea8d18b8ed4a94534cbcb ;;
  esac
}

if [ "$#" -ne 3 ]; then
  echo "usage: $0 OLD_SCHEMA NEW_SCHEMA REPORT" >&2
  exit 2
fi
old="$1" new="$2" report="$3"
for f in "$old" "$new"; do
  if [ ! -s "$f" ]; then
    echo "schema-compare: $f does not exist or is empty" >&2
    exit 1
  fi
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

install_schema_tools() {
  local os arch platform dir archive sum
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) arch="$(uname -m)" ;;
  esac
  platform="${os}-${arch}"
  sum="$(schema_tools_sha256 "$platform")"
  if [ -z "$sum" ]; then
    echo "schema-compare: no pinned schema-tools ${SCHEMA_TOOLS_VERSION} build for ${platform}; set SCHEMA_TOOLS" >&2
    return 1
  fi
  dir="${root}/bin/schema-tools-${SCHEMA_TOOLS_VERSION}"
  if [ ! -x "${dir}/schema-tools" ]; then
    archive="$(mktemp)"
    curl -fsSL --retry 3 -o "$archive" \
      "https://github.com/pulumi/schema-tools/releases/download/${SCHEMA_TOOLS_VERSION}/schema-tools-${SCHEMA_TOOLS_VERSION}-${platform}.tar.gz"
    if ! echo "${sum}  ${archive}" | sha256sum -c --status - 2>/dev/null &&
      ! echo "${sum}  ${archive}" | shasum -a 256 -c --status - 2>/dev/null; then
      echo "schema-compare: checksum mismatch for schema-tools ${SCHEMA_TOOLS_VERSION} (${platform})" >&2
      rm -f "$archive"
      return 1
    fi
    mkdir -p "$dir"
    tar -xzf "$archive" -C "$dir" schema-tools
    rm -f "$archive"
  fi
  echo "${dir}/schema-tools"
}

tool="${SCHEMA_TOOLS:-}"
if [ -z "$tool" ]; then
  tool="$(install_schema_tools)"
fi

mkdir -p "$(dirname "$report")"
"$tool" compare --provider docker-agent --old-path "$old" --new-path "$new" --max-changes 200 >"$report"

if grep -Eq '^Found [0-9]+ breaking changes?:' "$report"; then
  breaking=true
elif grep -Fq 'Looking good! No breaking changes found.' "$report"; then
  breaking=false
else
  echo "schema-compare: unexpected schema-tools output:" >&2
  cat "$report" >&2
  exit 1
fi

echo "breaking=${breaking}"
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "breaking=${breaking}" >>"$GITHUB_OUTPUT"
fi
