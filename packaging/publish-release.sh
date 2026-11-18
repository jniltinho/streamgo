#!/bin/sh
# Publishes the files of a release (the Packages workflow, on a v* tag): the five files of make package and
# SHA256SUMS, from <packages directory>, as the assets of <tag>'s GitHub release, with <tag>'s section of the
# changelog as the notes.
# usage: publish-release.sh <tag> <packages directory> [<changelog>]
#
# - No release yet: `gh release create` uploads the six to a draft and publishes it only when all are there.
# - A draft (an earlier run stopped): it is private, so each of the six is uploaded from this build (replacing what
#   is there), checked by downloading it again, anything else in it is removed, and the draft is then published.
# - A published release is never changed: people may have downloaded its files. With its six files, each matching
#   its SHA256SUMS, there is nothing to do; anything else stops here for a person to look at.
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
  for f in $have; do # what an earlier run or a person left in the draft
    case " $files SHA256SUMS " in
    *" $f "*) ;;
    *) $gh release delete-asset "$tag" "$f" --yes || die "cannot remove $f from the draft" ;;
    esac
  done
  $gh release edit "$tag" --draft=false || die "the draft of $tag is complete but was not published"
  echo "publish: the draft of $tag is complete and published"
  exit 0
fi

# a published release: never changed
missing=""
for f in $files SHA256SUMS; do
  has "$f" || missing="$missing $f"
done
[ -z "$missing" ] || die "the published release of $tag lacks:$missing; a published release is not changed: a person must look at it"
for f in $files SHA256SUMS; do
  remote "$f"
done
(cd "$tmp/remote" && sha256sum --check --strict --quiet SHA256SUMS) ||
  die "the files of the published release of $tag do not match its SHA256SUMS: a person must look at it"
echo "publish: the release of $tag is already published with its six files, which match its SHA256SUMS"
