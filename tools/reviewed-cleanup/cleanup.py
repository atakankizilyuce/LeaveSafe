"""Delete only the fixed, previously reviewed Actions artifacts after live checks."""
import argparse
import json
import os
from pathlib import Path
import urllib.error
import urllib.request

PROTECTED = {40361717359, 40353965419, 39083636875, 39073446655}
COUNTS = {"atakankizilyuce/leavesafe-app": (23, 73), "atakankizilyuce/LeaveSafe": (3, 34)}

def require(condition, message):
    if not condition:
        raise RuntimeError(message)

def request(repo, path, method="GET"):
    token = os.environ.get("GH_TOKEN")
    require(bool(token), "GH_TOKEN is required")
    url = "https://api.github.com/repos/" + repo + "/" + path
    req = urllib.request.Request(url, method=method, headers={
        "Authorization": "Bearer " + token,
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    with urllib.request.urlopen(req, timeout=60) as response:
        data = response.read()
        return json.loads(data) if data else None

def validate_scope(plan, repo):
    require(repo in COUNTS, "Unexpected repository")
    runs = plan["runs"]
    require(len(runs) == 26 and sum(len(r["artifacts"]) for r in runs) == 107, "Plan size changed")
    for entry in runs:
        require(entry["repository"] in COUNTS, "Repository outside reviewed scope")
        require(entry["id"] not in PROTECTED and entry["head_branch"] != "v0.1.11", "Protected release/run")
        require(entry["category"] in {"published-release-copy", "unpublished-rehearsal", "closed-pr-ci"}, "Unknown category")
    selected = [r for r in runs if r["repository"] == repo]
    require((len(selected), sum(len(r["artifacts"]) for r in selected)) == COUNTS[repo], "Repository scope changed")
    ids = [a["id"] for r in selected for a in r["artifacts"]]
    require(len(ids) == len(set(ids)), "Duplicate artifact IDs")
    return selected

def preflight(repo, entries):
    candidates = []
    for entry in entries:
        run = request(repo, "actions/runs/" + str(entry["id"]))
        workflow = "CI" if entry["category"] == "closed-pr-ci" else "Release"
        require(run["status"] == "completed" and run["name"] == workflow, "Run is active or workflow changed")
        require(all(run[k] == entry[k] for k in ("head_sha", "head_branch", "event")), "Run identity changed")
        category = entry["category"]
        if category == "published-release-copy":
            require(repo.endswith("/leavesafe-app") and run["conclusion"] == "success" and run["event"] == "push", "Invalid release run")
            release = request(repo, "releases/tags/" + entry["release"]["tag"])
            require(not release["draft"] and release["id"] == entry["release"]["id"] and release["tag_name"] == entry["head_branch"], "Release changed")
            for expected in entry["release"]["assets"]:
                matches = [a for a in release["assets"] if all(a.get(k) == expected[k] for k in ("id", "name", "size", "digest"))]
                require(len(matches) == 1, "Published asset changed: " + expected["name"])
        elif category == "unpublished-rehearsal":
            require(repo.endswith("/leavesafe-app") and run["conclusion"] == "success" and run["event"] == "workflow_dispatch", "Invalid rehearsal")
            jobs = request(repo, "actions/runs/" + str(entry["id"]) + "/jobs?per_page=100")
            require(jobs["total_count"] <= 100 and any(j["name"] == "Publish release" and j["conclusion"] == "skipped" for j in jobs["jobs"]), "Rehearsal published or pagination changed")
        else:
            require(repo.endswith("/LeaveSafe") and entry["pr_number"] in (147, 149, 150), "PR outside scope")
            pr = request(repo, "pulls/" + str(entry["pr_number"]))
            require(pr["state"] == "closed" and pr["head"]["ref"] == entry["head_branch"], "PR reopened or changed")
        actual = request(repo, "actions/runs/" + str(entry["id"]) + "/artifacts?per_page=100")
        require(actual["total_count"] <= 100, "Unexpected pagination")
        by_id = {a["id"]: a for a in actual["artifacts"]}
        for expected in entry["artifacts"]:
            item = by_id.get(expected["id"])
            if item is None:
                print("ALREADY_ABSENT " + str(expected["id"]), flush=True)
                continue
            require(all(item[k] == expected[k] for k in ("id", "name", "size_in_bytes")), "Artifact metadata changed")
            if not item["expired"]:
                candidates.append({"repository": repo, "run": entry["id"], **expected})
    return candidates

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--repository", default=os.environ.get("GITHUB_REPOSITORY", ""))
    args = parser.parse_args()
    plan = json.loads(Path(__file__).with_name("reviewed-plan.json").read_text(encoding="utf-8"))
    entries = validate_scope(plan, args.repository)
    candidates = preflight(args.repository, entries)
    print("PREFLIGHT " + json.dumps({"repository": args.repository, "artifacts": len(candidates), "bytes": sum(c["size_in_bytes"] for c in candidates)}), flush=True)
    if not args.apply:
        print("Preview only; no deletion requested.", flush=True)
        return
    for item in candidates:
        request(args.repository, "actions/artifacts/" + str(item["id"]), "DELETE")
        print("DELETED " + json.dumps(item, separators=(",", ":")), flush=True)
    for entry in entries:
        expected = {c["id"] for c in candidates if c["run"] == entry["id"]}
        if expected:
            actual = request(args.repository, "actions/runs/" + str(entry["id"]) + "/artifacts?per_page=100")
            require(not expected.intersection(a["id"] for a in actual["artifacts"]), "Deletion verification failed")
    print("VERIFIED " + json.dumps({"repository": args.repository, "artifacts": len(candidates), "bytes": sum(c["size_in_bytes"] for c in candidates)}), flush=True)

if __name__ == "__main__":
    main()
