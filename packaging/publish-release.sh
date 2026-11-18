#!/bin/sh
# Publishes the files of a release (the Packages workflow, on a v* tag): the five files of make package and
# SHA256SUMS, from <packages directory>, as the assets of <tag>'s GitHub release, with <tag>'s section of the
# changelog as the notes.
# usage: publish-release.sh <tag> <packages directory> [<changelog>]
#
# - No release yet: `gh release create` uploads the six to a draft and publishes it only when all are there.
# - A draft (an earlier run stopped): it is private, so each of the six is uploaded from this build (replacing what
#   is there), checked by downloading it again, and the draft is then published.
# - A published release that lacks files (made by hand, or by an earlier version with four files): the files it has
#   are kept, each checked against the SHA256SUMS it has (or, when that does not list it, against this build's file:
#   an interrupted run of the same build); a mismatch stops here for a person to look at. The missing files are
#   uploaded and checked, and only then is SHA256SUMS replaced: uploaded as SHA256SUMS.next, checked, the old one
#   deleted and the new one renamed SHA256SUMS. A run stopped in between leaves SHA256SUMS.next, which the next run
#   reads as the old manifest. A release is never left without a manifest of its files.
# GH (default gh) is the GitHub CLI (packaging_test.go runs this with a fake one); GH_REPO names the repository.
set -eu

usage="usage: publish-release.sh <tag> <packages directory> [<changelog>]"
tag=${1:?$usage}
dir=${2:?$usage}
changelog=${3:-CHANGELOG.md}
gh=${GH:-gh}

die() {
  echo "publish: $*" >&2
  exit 1
}

printf '%s\n' "$tag" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+' || die "the tag '$tag' is not vX.Y.Z"
v=${tag#v}
files="streamgo_${v}_amd64.deb streamgo-${v}-1.x86_64.rpm streamgo_${v}_linux_amd64.tar.gz streamgo_${v}_linux_amd64 streamgo-server_${v}_linux_amd64.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

awk -v v="$v" '/^## \[/ { p = index($0, "## [" v "]") == 1; next } p' "$changelog" >"$tmp/notes.md"
[ -s "$tmp/notes.md" ] || die "$changelog has no section for $v"
for f in $files SHA256SUMS; do
  [ -f "$dir/$f" ] || die "$dir lacks $f"
done
(cd "$dir" && sha256sum --check --strict --quiet SHA256SUMS) || die "$dir/SHA256SUMS does not match its files"

# remote <name>: the release's asset <name>, downloaded to $tmp/remote/<name>
remote() {
  mkdir -p "$tmp/remote"
  rm -f "$tmp/remote/$1"
  $gh release download "$tag" --pattern "$1" --dir "$tmp/remote" || die "cannot download $1"
  [ -f "$tmp/remote/$1" ] || die "cannot download $1"
}
assets() { $gh release view "$tag" --json assets --jq '.assets[].name'; }
has() { printf '%s\n' "$have" | grep -qxF "$1"; }

if ! $gh release view "$tag" --json isDraft --jq .isDraft >"$tmp/draft" 2>"$tmp/view.err"; then
  grep -q 'release not found' "$tmp/view.err" || { cat "$tmp/view.err" >&2; die "cannot read the release of $tag"; }
  cd "$dir"
  # shellcheck disable=SC2086 # the five names hold no blank
  $gh release create "$tag" $files SHA256SUMS --verify-tag --title "StreamGo $tag" --notes-file "$tmp/notes.md"
  exit 0
fi
have=$(assets) || die "cannot list the assets of $tag"

if [ "$(cat "$tmp/draft")" = true ]; then
  echo "publish: $tag is a draft: uploading the six files of this build and publishing it"
  # shellcheck disable=SC2086
  (cd "$dir" && $gh release upload "$tag" $files SHA256SUMS --clobber) || die "the upload to the draft failed"
  for f in $files SHA256SUMS; do
    remote "$f"
    cmp -s "$tmp/remote/$f" "$dir/$f" || die "the draft's $f is not the file uploaded"
  done
  $gh release edit "$tag" --draft=false || die "the draft of $tag is complete but was not published"
  echo "publish: the draft of $tag is complete and published"
  exit 0
fi

# a published release: what it has is checked against the manifest it has
old=""
if has SHA256SUMS; then
  old=SHA256SUMS
elif has SHA256SUMS.next; then
  old=SHA256SUMS.next
fi
[ -n "$old" ] && remote "$old"
mkdir -p "$tmp/release"
missing=""
for f in $files; do
  if ! has "$f"; then
    missing="$missing $f"
    cp "$dir/$f" "$tmp/release/$f"
    continue
  fi
  remote "$f"
  mv "$tmp/remote/$f" "$tmp/release/$f"
  want=""
  [ -z "$old" ] || want=$(awk -v f="$f" '$2 == f || $2 == "*" f { print $1 }' "$tmp/remote/$old")
  got=$(sha256sum "$tmp/release/$f" | cut -d' ' -f1)
  if [ -n "$want" ]; then
    [ "$got" = "$want" ] || die "the release's $f does not match its $old: a person must look at it"
  else
    cmp -s "$tmp/release/$f" "$dir/$f" || die "the release's $f is listed in no manifest and is not this build's: a person must look at it"
  fi
done

if [ -n "$missing" ]; then
  echo "publish: adding to the release of $tag:$missing"
  # shellcheck disable=SC2086
  (cd "$dir" && $gh release upload "$tag" $missing) || die "the upload of$missing failed; SHA256SUMS is unchanged"
  have=$(assets) || die "cannot list the assets of $tag"
  for f in $files; do
    has "$f" || die "the release still lacks $f"
  done
  for f in $missing; do
    remote "$f"
    cmp -s "$tmp/remote/$f" "$dir/$f" || die "the release's $f is not the file uploaded"
  done
fi

# shellcheck disable=SC2086
(cd "$tmp/release" && sha256sum $files >SHA256SUMS)
if has SHA256SUMS && remote SHA256SUMS && cmp -s "$tmp/remote/SHA256SUMS" "$tmp/release/SHA256SUMS"; then
  echo "publish: the release of $tag is complete"
  exit 0
fi

echo "publish: replacing SHA256SUMS of $tag"
mkdir -p "$tmp/next"
cp "$tmp/release/SHA256SUMS" "$tmp/next/SHA256SUMS.next"
if ! { has SHA256SUMS.next && remote SHA256SUMS.next && cmp -s "$tmp/remote/SHA256SUMS.next" "$tmp/next/SHA256SUMS.next"; }; then
  (cd "$tmp/next" && $gh release upload "$tag" SHA256SUMS.next --clobber) || die "the upload of SHA256SUMS.next failed; SHA256SUMS is unchanged"
fi
remote SHA256SUMS.next
cmp -s "$tmp/remote/SHA256SUMS.next" "$tmp/release/SHA256SUMS" || die "SHA256SUMS.next is not the file uploaded; SHA256SUMS is unchanged"
id=$($gh release view "$tag" --json assets --jq '.assets[] | select(.name == "SHA256SUMS.next") | .id') || die "cannot read the id of SHA256SUMS.next"
[ -n "$id" ] || die "the release has no SHA256SUMS.next"
if has SHA256SUMS; then
  $gh release delete-asset "$tag" SHA256SUMS --yes || die "cannot delete the old SHA256SUMS; SHA256SUMS.next is the new one"
fi
i=0
until $gh api -X PATCH "repos/{owner}/{repo}/releases/assets/$id" -f name=SHA256SUMS >/dev/null; do
  i=$((i + 1))
  [ "$i" -lt 3 ] || die "SHA256SUMS.next was not renamed SHA256SUMS: it is the release's manifest until a run renames it"
  sleep "${PUBLISH_RETRY_DELAY:-5}"
done
remote SHA256SUMS
cmp -s "$tmp/remote/SHA256SUMS" "$tmp/release/SHA256SUMS" || die "the release's SHA256SUMS is not the one written"
(cd "$tmp/release" && sha256sum --check --strict --quiet "$tmp/remote/SHA256SUMS") || die "the release's files do not match its SHA256SUMS"
echo "publish: the release of $tag is complete:$missing added, SHA256SUMS replaced and checked"
