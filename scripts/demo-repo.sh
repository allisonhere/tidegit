#!/usr/bin/env bash
# Build a throwaway repository with enough history to exercise TideGit's
# History and Branches screens: hundreds of commits, concurrent branch lanes,
# merges of every shape, tags, several remotes and a spread of dates.
#
#   scripts/demo-repo.sh [DESTINATION]        # default /tmp/tidegit-demo
#   ./bin/tidegit /tmp/tidegit-demo
#
# The result is deterministic apart from its dates, which are anchored to the
# current time so relative ages stay realistic. The destination is deleted and
# rebuilt on every run, so never point it at anything you care about.
set -euo pipefail

DEST=${1:-/tmp/tidegit-demo}
TRUNK_COMMITS=${TRUNK_COMMITS:-260}

case "$DEST" in
  ""|/|"$HOME") echo "refusing to build a demo repository at $DEST" >&2; exit 2 ;;
esac
if [ -e "$DEST" ] && [ ! -e "$DEST/.git" ]; then
  echo "$DEST exists and is not a Git repository; refusing to delete it" >&2
  exit 2
fi

rm -rf "$DEST"
mkdir -p "$DEST"
cd "$DEST"

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git init -q -b main .
git config user.name "Allison Bayless"
git config user.email "allie@example.invalid"
git config advice.detachedHead false

# ---------------------------------------------------------------- identities
AUTHORS=(
  "Allison Bayless|allie@example.invalid"
  "Rowan Fields|rowan@example.invalid"
  "Priya Raman|priya@example.invalid"
  "Tomás Okafor|tomas@example.invalid"
  "Wei Zhang|wei@example.invalid"
  "Dana Kowalski|dana@example.invalid"
)

# Dates walk forward from far enough back that the newest commits are hours old
# and the oldest are more than a year old, so every age format is visible. The
# estimate covers the branch and merge commits the trunk count does not, so the
# newest commit lands close to now rather than in the future.
STEP_AVG=36
ESTIMATED_COMMITS=$(( TRUNK_COMMITS + 40 ))
START=$(( $(date +%s) - ESTIMATED_COMMITS * STEP_AVG * 3600 ))
CLOCK=$START
TICK=0

# tick advances the clock by a varying but repeatable amount.
tick() {
  local hours=$(( 8 + (TICK * 13) % 56 ))
  CLOCK=$(( CLOCK + hours * 3600 ))
  TICK=$(( TICK + 1 ))
  # Never hand Git a future date: an age computed from one reads as "now".
  local ceiling=$(( $(date +%s) - 1800 ))
  if [ $CLOCK -gt $ceiling ]; then CLOCK=$ceiling; fi
}

# author rotates deterministically, with the owner taking most of the trunk.
author_for() {
  local n=$1 pick
  if [ $(( n % 3 )) -eq 0 ]; then pick=0; else pick=$(( (n * 7) % ${#AUTHORS[@]} )); fi
  echo "${AUTHORS[$pick]}"
}

FILES=(
  internal/git/runner.go internal/git/status.go internal/git/patch.go
  internal/git/staging.go internal/git/commit.go internal/git/history.go
  internal/ui/model.go internal/ui/view.go internal/ui/diff.go
  internal/ui/graph.go docs/architecture.md README.md
)

VERBS=(Add Fix Refactor Simplify Document Tighten Rework Guard Extract Inline Bound Cache)
NOUNS=(
  "the porcelain parser" "hunk identity" "the diff renderer" "lane assignment"
  "the mutation gate" "ref decoration parsing" "the status scan" "commit paging"
  "the inspector layout" "background continuity" "the command runner"
  "output limits" "the staging service" "branch tracking" "the graph model"
)

N=0

# touch_path picks the file a commit edits. Work on the trunk spreads across
# the shared files; a branch writes under its own directory, so a branch and
# the trunk never edit the same line and merges stay clean.
touch_path() {
  local branch slug
  branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null || echo detached)
  if [ "$branch" = main ] || [ "$branch" = detached ]; then
    echo "${FILES[$(( N % ${#FILES[@]} ))]}"
    return
  fi
  slug=${branch//\//_}
  local names=(core.go plumbing.go notes.md)
  echo "internal/${slug}/${names[$(( N % 3 ))]}"
}

# commit <subject> [body]
commit() {
  local subject=$1 body=${2:-}
  local ident name email
  ident=$(author_for $N); name=${ident%%|*}; email=${ident##*|}
  tick
  local stamp="@$CLOCK +0000"
  local file; file=$(touch_path)
  mkdir -p "$(dirname "$file")"
  printf '// %s\nline %d of %s\n' "$subject" "$N" "$file" >> "$file"
  git add -A
  if [ -n "$body" ]; then
    printf '%s\n\n%s\n' "$subject" "$body" |
      GIT_AUTHOR_NAME="$name" GIT_AUTHOR_EMAIL="$email" \
      GIT_COMMITTER_NAME="$name" GIT_COMMITTER_EMAIL="$email" \
      GIT_AUTHOR_DATE="$stamp" GIT_COMMITTER_DATE="$stamp" \
      git commit -q -F -
  else
    GIT_AUTHOR_NAME="$name" GIT_AUTHOR_EMAIL="$email" \
    GIT_COMMITTER_NAME="$name" GIT_COMMITTER_EMAIL="$email" \
    GIT_AUTHOR_DATE="$stamp" GIT_COMMITTER_DATE="$stamp" \
    git commit -q -m "$subject"
  fi
  N=$(( N + 1 ))
}

# auto makes a commit with a generated subject.
auto() {
  local verb=${VERBS[$(( N % ${#VERBS[@]} ))]}
  local noun=${NOUNS[$(( (N * 5) % ${#NOUNS[@]} ))]}
  commit "$verb $noun"
}

# merge_into <branch> <message>
merge_into() {
  local branch=$1 message=$2 ident name email
  ident=$(author_for $N); name=${ident%%|*}; email=${ident##*|}
  tick
  local stamp="@$CLOCK +0000"
  GIT_AUTHOR_NAME="$name" GIT_AUTHOR_EMAIL="$email" \
  GIT_COMMITTER_NAME="$name" GIT_COMMITTER_EMAIL="$email" \
  GIT_AUTHOR_DATE="$stamp" GIT_COMMITTER_DATE="$stamp" \
  git merge -q --no-ff -m "$message" "$branch"
  N=$(( N + 1 ))
}

echo "building $TRUNK_COMMITS-commit history in $DEST"

# ------------------------------------------------------------------- history
commit "Set up the repository skeleton" \
  "The first commit. Everything else grows from here."
for _ in $(seq 1 8); do auto; done
git tag v0.1.0

FEATURES=(
  hunk-staging commit-editor history-view branch-browser graph-lanes
  diff-viewer ref-badges palette-commands theme-picker background-fix
)
feature_at=0

# The trunk alternates between straight work and a feature branch that forks,
# runs for a while alongside it and merges back, so several lanes are open at
# once rather than one branch at a time.
while [ $N -lt $TRUNK_COMMITS ]; do
  for _ in $(seq 1 $(( 3 + N % 4 ))); do
    [ $N -lt $TRUNK_COMMITS ] || break
    auto
  done
  [ $N -lt $TRUNK_COMMITS ] || break

  name="feature/${FEATURES[$(( feature_at % ${#FEATURES[@]} ))]}-$feature_at"
  feature_at=$(( feature_at + 1 ))
  git switch -qc "$name"
  for _ in $(seq 1 $(( 2 + N % 5 ))); do auto; done

  # Every third feature takes a merge from the trunk before merging back,
  # which is what produces criss-cross lanes rather than tidy bubbles.
  if [ $(( feature_at % 3 )) -eq 0 ]; then
    merge_into main "Merge main into $name"
    auto
  fi
  git switch -q main
  # Keep the trunk moving so the merge actually has two sides.
  auto
  merge_into "$name" "Merge $name into main"

  case $(( feature_at % 4 )) in
    0) git branch -q -d "$name" ;;  # tidied up after merging
  esac
done

git tag -a v0.2.0 -m "Second milestone"

# ------------------------------------------------------- an octopus merge
# Three short branches merged at once, so the graph has to draw a node with
# more than two parents.
for side in alpha beta gamma; do
  git switch -qc "topic/$side" main
  commit "Explore $side approach"
  git switch -q main
done
tick
GIT_AUTHOR_DATE="@$CLOCK +0000" GIT_COMMITTER_DATE="@$CLOCK +0000" \
  git merge -q --no-ff -m "Merge three exploratory topics" \
  topic/alpha topic/beta topic/gamma
git branch -q -D topic/alpha topic/beta topic/gamma

# ------------------------------------------------- long-running side branch
git switch -qc develop main~12
for _ in $(seq 1 14); do auto; done
git switch -q main
merge_into develop "Merge develop into main"

# ------------------------------------------------------ release + hotfix
git switch -qc release/1.0 main~4
for _ in $(seq 1 3); do auto; done
git tag -a v1.0.0 -m "First stable release"
git switch -qc hotfix/token-refresh
commit "Fix token refresh on a expired session" \
  "The refresh path reused the expired token when the clock skewed
backwards, so a session that should have recovered silently failed.

Reported against v1.0.0."
git switch -q main
merge_into hotfix/token-refresh "Merge hotfix/token-refresh into main"
git tag v1.0.1

# --------------------------------------------------- branches left unmerged
for spike in wip/inline-blame spike/rebase-ui experiment/two-row-graph; do
  git switch -qc "$spike" main
  for _ in $(seq 1 3); do auto; done
  git switch -q main
done

# An unrelated history, which has no merge base with anything.
git switch -q --orphan vendor/imported
git rm -rqf --cached . 2>/dev/null || true
rm -rf internal docs README.md 2>/dev/null || true
commit "Import vendored terminal library" "Unrelated history: no common ancestor with main."
for _ in $(seq 1 2); do auto; done
git switch -q main

# ------------------------------------------------------- shapes for the UI
commit "Rename the runner to make room for the pooled implementation"
git mv internal/git/runner.go internal/git/command.go
tick
GIT_AUTHOR_DATE="@$CLOCK +0000" GIT_COMMITTER_DATE="@$CLOCK +0000" \
  git commit -q -m "Rename runner.go to command.go"

mkdir -p assets
printf '\x00\x01\x02binary payload\x03\x04\x00\xff' > assets/logo.bin
commit "Add a binary asset so the inspector has one to describe"

git rm -q docs/architecture.md
tick
GIT_AUTHOR_DATE="@$CLOCK +0000" GIT_COMMITTER_DATE="@$CLOCK +0000" \
  git commit -q -m "Delete the architecture note, now folded into the README"

commit "Add a deliberately long commit subject that keeps going well past any sensible column limit so clipping can be seen" \
  "Some commits really are written like this."

commit "Record the inspector layout" \
  "The inspector needs a strong hierarchy: subject first, then identity,
then the message, then the changed files.

This body exists so the right-hand pane has several paragraphs to
render, including blank lines between them and lines long enough to
need wrapping at a narrow width.

    It also has an indented block, which should survive unwrapped.

Unicode: 海 · ünïcödé · 🌊"

# --------------------------------------------------------------- remotes
# Remote-tracking refs are ordinary refs; creating them directly keeps this
# offline. Two remotes, at different distances from the local branches.
git remote add origin "$DEST/../tidegit-demo-origin.git"
git remote add upstream "$DEST/../tidegit-demo-upstream.git"

git update-ref refs/remotes/origin/main "$(git rev-parse main~3)"
git update-ref refs/remotes/origin/develop "$(git rev-parse develop)"
git update-ref refs/remotes/origin/release/1.0 "$(git rev-parse release/1.0)"
git update-ref refs/remotes/origin/wip/inline-blame "$(git rev-parse wip/inline-blame~1)"
git update-ref refs/remotes/upstream/main "$(git rev-parse main~18)"
# A branch whose upstream has since been deleted, so it reports "gone".
git update-ref refs/remotes/origin/spike/rebase-ui "$(git rev-parse spike/rebase-ui)"

git branch -q --set-upstream-to=origin/main main
git branch -q --set-upstream-to=origin/develop develop
git branch -q --set-upstream-to=origin/release/1.0 release/1.0
git branch -q --set-upstream-to=origin/wip/inline-blame wip/inline-blame
git branch -q --set-upstream-to=origin/spike/rebase-ui spike/rebase-ui
git update-ref -d refs/remotes/origin/spike/rebase-ui

# ------------------------------------------------------- working-tree state
printf '\n// an unstaged edit\n' >> internal/ui/view.go
printf '\n// a staged edit\n' >> internal/ui/model.go
git add internal/ui/model.go
printf 'scratch notes\n' > NOTES.md

cat <<SUMMARY

built $(git rev-list --all --count) commits in $DEST
  $(git for-each-ref --format='%(refname)' refs/heads | wc -l) local branches, \
$(git for-each-ref --format='%(refname)' refs/remotes | wc -l) remote-tracking, \
$(git tag | wc -l) tags
  widest graph point: $(git log --all --format='%p' | awk '{print NF}' | sort -rn | head -1) parents
  oldest: $(git log --all --reverse --format='%ad' --date=short | head -1)
  newest: $(git log --all --format='%ad' --date=short | head -1)

  ./bin/tidegit $DEST
SUMMARY
