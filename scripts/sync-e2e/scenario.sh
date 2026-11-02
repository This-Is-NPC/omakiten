#!/usr/bin/env bash
# Drives the sync end-to-end scenario against the running compose stack.
set -euo pipefail

SERVER_URL=http://server:8787
step_name=""

step() {
  step_name="$1"
}

pass() { printf 'PASS  %s\n' "$step_name"; }

fail() {
  printf 'FAIL  %s: %s\n' "$step_name" "$*" >&2
  exit 1
}

# exec_in SERVICE DIR ARGS... runs a command in the service's container; stdin is closed so
# callers inside read loops keep their input.
exec_in() {
  local service="$1" dir="$2"
  shift 2
  docker exec -w "$dir" "okt-sync-e2e-$service-1" "$@" </dev/null
}

# okt SERVICE DIR ARGS... runs okt in a container and prints the raw JSON envelope.
okt() {
  local service="$1" dir="$2"
  shift 2
  exec_in "$service" "$dir" okt "$@"
}

# ok SERVICE DIR ARGS... runs okt, asserts .ok == true, and prints .data.
ok() {
  local out
  out="$(okt "$@")" || true
  if [[ "$(jq -r '.ok' <<<"$out" 2>/dev/null)" != true ]]; then
    fail "okt ${*:3} on $1 did not return ok: $out"
  fi
  jq -c '.data' <<<"$out"
}

expect() {
  local what="$1" got="$2" want="$3"
  [[ "$got" == "$want" ]] || fail "$what: got [$got], want [$want]"
}

# task_id SERVICE DIR TITLE prints the id of the task with that title.
task_id() {
  local id
  id="$(ok "$1" "$2" list | jq -r --arg t "$3" '[.tasks[] | select(.title == $t) | .id][0] // empty')"
  [[ -n "$id" ]] || fail "no task titled \"$3\" on $1"
  printf '%s' "$id"
}

# snapshot SERVICE DIR prints the project's tasks normalised by title.
snapshot() {
  local service="$1" dir="$2" list id shows=()
  list="$(ok "$service" "$dir" list)"
  while read -r id; do
    shows+=("$(ok "$service" "$dir" task show "$id")")
  done < <(jq -r '.tasks[].id' <<<"$list")
  jq -rn --argjson list "$list" --slurpfile shows <(printf '%s\n' "${shows[@]:-}" | grep -v '^$' || true) '
    ($list.tasks | map({key: (.id | tostring), value: .title}) | from_entries) as $title
    | [$list.tasks[] as $t
      | ($shows | map(select(.task.id == $t.id)) | first // {}) as $s
      | [$t.title, $t.bucket_key,
         (if $t.parent_id then $title[$t.parent_id | tostring] else "-" end),
         ([($s.dependencies // [])[] | $title[.depends_on_task_id | tostring]] | sort | join(",")),
         ([($s.comments // [])[] | .body] | sort | join(";"))]
      | join(" | ")]
    | sort | .[]'
}

# same_state fails unless both machines hold the same normalised project state.
same_state() {
  local sa sb
  sa="$(snapshot a /work/p)"
  sb="$(snapshot b /work/p)"
  [[ "$sa" == "$sb" ]] || fail "state differs"$'\n'"--- a"$'\n'"$sa"$'\n'"--- b"$'\n'"$sb"
}

sync_both() {
  local m
  for m in "$@"; do
    ok "$m" /work/p sync >/dev/null
  done
}

# --- 1: project P created and synced on a -----------------------------------
step "1 project P is created and pushed from a"
exec_in a / sh -c 'mkdir -p /work/p /work/l && git init -q /work/p && git init -q /work/l'
ok a /work/p init --name P --slug p >/dev/null
ok a /work/p task create --title T1 >/dev/null
ok a /work/p task create --title T2 >/dev/null
ok a /work/p task create --title Child --parent "$(task_id a /work/p T1)" >/dev/null
ok a /work/p comment add "$(task_id a /work/p T1)" -b "note on T1" >/dev/null
ok a /work/p depend add "$(task_id a /work/p T1)" --on "$(task_id a /work/p T2)" >/dev/null
ok a /work/p project edit --sync-scope synced >/dev/null
ok a /work/p remote set "$SERVER_URL" >/dev/null
pushed="$(ok a /work/p sync | jq '[.projects[].pushed] | add')"
((pushed > 0)) || fail "pushed = $pushed"
pass

# --- 2: local project never reaches the server ------------------------------
step "2 local project L stays off the server"
ok a /work/l init --name L --slug l >/dev/null
ok a /work/l task create --title LocalOnly >/dev/null
sync_both a
ok b /work remote set "$SERVER_URL" >/dev/null
remote="$(ok b /work remote projects)"
expect "server project count" "$(jq '.projects | length' <<<"$remote")" 1
uid="$(jq -r '.projects[0].uid' <<<"$remote")"
[[ -n "$uid" && "$uid" != null ]] || fail "no uid for P in $remote"
pass

# --- 3: b clones P and sees equal state --------------------------------------
step "3 b clones P and matches a"
exec_in b / sh -c 'mkdir -p /work/p && git init -q /work/p'
ok b /work projects clone "$uid" --root /work/p >/dev/null
expect "clone content" "$(snapshot b /work/p | wc -l)" 3
same_state
pass

# --- 4: offline edits converge ------------------------------------------------
step "4 offline edits on a and b converge"
ok a /work/p edit "$(task_id a /work/p T1)" --title "T1 renamed" >/dev/null
t2b="$(task_id b /work/p T2)"
ok b /work/p comment add "$t2b" -T self-branch -b "branch for T2" >/dev/null
ok b /work/p move "$t2b" --to dev >/dev/null
ok b /work/p comment add "$t2b" -b "offline note on T2" >/dev/null
sync_both a b a
same_state
snap="$(snapshot a /work/p)"
grep -q '^T1 renamed ' <<<"$snap" || fail "rename missing: $snap"
grep -q '^T2 | dev |' <<<"$snap" || fail "T2 move missing: $snap"
grep -q 'offline note on T2' <<<"$snap" || fail "T2 comment missing: $snap"
pass

# --- 5: later edit wins --------------------------------------------------------
step "5 conflicting title edit: later edit wins"
ok a /work/p edit "$(task_id a /work/p "T1 renamed")" --title "title from a" >/dev/null
sleep 1.2
ok b /work/p edit "$(task_id b /work/p "T1 renamed")" --title "title from b" >/dev/null
sync_both a b a
for m in a b; do
  expect "T1 title on $m" "$(ok $m /work/p list | jq -r '[.tasks[].title | select(startswith("title from"))] | join(",")')" "title from b"
done
same_state
pass

# --- 6: delete propagates -------------------------------------------------------
step "6 delete on a reaches b"
t2a="$(task_id a /work/p T2)"
ok a /work/p move "$t2a" --to backlog >/dev/null
ok a /work/p delete "$t2a" --confirm >/dev/null
sync_both a b
expect "T2 on b" "$(ok b /work/p list | jq '[.tasks[] | select(.title == "T2")] | length')" 0
same_state
pass

# --- 7: bad token refused --------------------------------------------------------
step "7 wrong token is refused"
out="$(exec_in b /work env OMAKITEN_SYNC_TOKEN=wrong okt remote projects || true)"
expect "ok flag" "$(jq -r '.ok' <<<"$out")" false
pass

# --- 8: server restart keeps data -------------------------------------------------
step "8 data survives a server restart"
docker compose restart server >/dev/null 2>&1
docker compose up -d --wait server >/dev/null 2>&1
ok b /work/p task create --title T3 >/dev/null
sync_both b a
expect "T3 on a" "$(ok a /work/p list | jq '[.tasks[] | select(.title == "T3")] | length')" 1
same_state
pass

# --- 9: a removed tag can be added again ------------------------------------------
step "9 a tag removed and added again reaches the other machine"
tag_count() {
  ok "$1" /work/p tag list --entity-type task --entity-id "$(task_id "$1" /work/p T3)" |
    jq '[.. | objects | select(.name? == "flaky")] | length'
}
ok a /work/p tag add --entity-type task --entity-id "$(task_id a /work/p T3)" --name flaky >/dev/null
sync_both a b
expect "tag on b after add" "$(tag_count b)" 1
flaky_id="$(ok a /work/p tag list --entity-type task --entity-id "$(task_id a /work/p T3)" |
  jq -r '[.. | objects | select(.name? == "flaky") | .id][0]')"
ok a /work/p tag remove --entity-type task --entity-id "$(task_id a /work/p T3)" --tag-id "$flaky_id" --confirm >/dev/null
sync_both a b
expect "tag on b after remove" "$(tag_count b)" 0
ok a /work/p tag add --entity-type task --entity-id "$(task_id a /work/p T3)" --name flaky >/dev/null
sync_both a b
expect "tag on b after re-add" "$(tag_count b)" 1
pass

printf 'all steps passed\n'
