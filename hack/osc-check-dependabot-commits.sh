#!/usr/bin/env bash

set -euo pipefail

declare -r branch_prefix="sync-"
declare -r branch_delimiter="SourceBranch:"

function fetch_merge_requests() {
  # Using `-r` with jq instead of `-er` to avoid pipeline failures when no existing merge requests are found.
  # This ensures the function returns an empty string in such cases instead.
  gitlab -o json -f title,source_branch,state \
    project-merge-request list \
    --project-id="$PROJECT_ID" \
    --author-id="$AUTHOR_ID" \
    --updated-after="$(date -d "@$(($(date +%s) - 14 * 24 * 60 * 60))" -Iseconds)" \
    --get-all |
    jq -r ".[] | select(.state == (\"opened\", \"closed\")) | .title + \"${branch_delimiter}\" + .state + \":\" + .source_branch"
}

function should_process_commit() {
  local commit_message="$1"

  # Keeping the -q, its not satisfying the if condition and ultimately the pipeline is failing. Hence, using /dev/null 2>&1 here.
  if git --no-pager log --pretty=format:"%s%n" | grep -F "$commit_message" >/dev/null 2>&1; then
    echo -e "\033[32mCommit message '$commit_message' already exists in the repo. Skipping...\033[0m"
    return 1
  fi

  if echo "$existing_mrs" | grep -qF "$commit_message"; then
    echo -e "\033[32mMerge request with title '$commit_message' already exists. Skipping...\033[0m"
    return 1
  fi

  return 0
}

function create_merge_request() {
  local branch_name="$1"
  local commit_message="$2"
  local commit_hash="$3"

  local mr_title="[$current_date] Sync Commit #$series_number: $commit_message"

  gitlab --timeout="$GITLAB_TIMEOUT" project-merge-request create \
    --project-id="$PROJECT_ID" \
    --source-branch="${branch_name}" \
    --target-branch=osc/main \
    --title="${mr_title}" \
    --description="Please merge ${commit_hash} of upstream" \
    --squash=true \
    --remove-source-branch=true \
    --reviewer-ids="$REVIEWER_IDS" >/dev/null
}

function handle_commit() {
  local commit_hash="$1"
  local commit_message="$2"

  local branch_name="${branch_prefix}${commit_hash}"

  if git show-ref "$branch_name" --quiet; then
    create_merge_request "$branch_name" "$commit_message" "$commit_hash"
    return 0
  fi

  git switch osc/main --quiet

  git switch -c "$branch_name" --quiet
  if ! git cherry-pick "$commit_hash" --ff --quiet &>/dev/null; then
    # add conflicted changes
    mapfile -t files < <(git diff --name-only --diff-filter=U)
    git add -- "${files[@]}"
    git cherry-pick --continue
  fi
  git push origin "$branch_name" --quiet

  create_merge_request "$branch_name" "$commit_message" "$commit_hash"
}

function cleanup_orphan_branch() {
  local -r branch_name="$1"
  if git show-ref "$branch_name" --quiet; then
    echo "cleanup sync branch: ${branch_name}"
    git push -d origin "${branch_name}"
  fi
}

echo "Setting up Git configuration..."
git config --global user.email "noreply@gitlab.devops.telekom.de"
git config --global user.name "sync user"
git remote set-url origin "https://oauth2:${CI_PUSH_TOKEN}@gitlab.devops.telekom.de/cas-devs/osc/upstream/ironcore-dev/libvirt-provider.git"
git fetch --all --quiet

git switch main --quiet

mapfile -t commits < <(git --no-pager log --since="7 days ago" --pretty="format:%H %s")

commits_count=${#commits[@]}
if [[ $commits_count -eq 0 ]]; then
  echo "No commits found in the last 7 days."
  exit 0
fi

existing_mrs="$(fetch_merge_requests)"
current_date="$(date +"%Y-%m-%d")"

new_commits=()
for commit in "${commits[@]}"; do
  commit_hash=$(echo "$commit" | cut -d' ' -f1)
  commit_message=$(echo "$commit" | cut -d' ' -f2-)

  git switch osc/main --quiet

  if should_process_commit "$commit_message"; then
    new_commits+=("$commit")
  fi
done

new_commits_count=${#new_commits[@]}
if [[ $new_commits_count -eq 0 ]]; then
  echo "No new commits to create merge requests for."
  exit 0
fi

series_number=$new_commits_count

for commit in "${new_commits[@]}"; do
  commit_hash=$(echo "$commit" | cut -d' ' -f1)
  commit_message=$(echo "$commit" | cut -d' ' -f2-)

  handle_commit "$commit_hash" "$commit_message"
  ((series_number--))
done

IFS=$'\n'
for branch in $(echo "${existing_mrs}" | grep -Eo "${branch_delimiter}closed:${branch_prefix}[a-z0-9]+" | cut -d':' -f 3); do
  cleanup_orphan_branch "${branch}"
done
