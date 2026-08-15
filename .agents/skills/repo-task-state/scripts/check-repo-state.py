#!/usr/bin/env python3
"""Return compact, deterministic repository and delivery state as JSON."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any


def run(command: list[str], *, timeout: int = 20, cwd: Path | None = None) -> tuple[int, str]:
    try:
        completed = subprocess.run(
            command,
            cwd=cwd,
            check=False,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired):
        return 1, ""
    return completed.returncode, completed.stdout.strip()


def git_value(root: Path, *args: str) -> str:
    code, output = run(["git", *args], cwd=root)
    return output if code == 0 else ""


def gh_json(args: list[str]) -> tuple[bool, Any]:
    if shutil.which("gh") is None:
        return False, None
    code, output = run(["gh", *args], timeout=30)
    if code != 0 or not output:
        return False, None
    try:
        return True, json.loads(output)
    except json.JSONDecodeError:
        return False, None


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--mode", choices=("handoff", "preflight", "merge"), required=True)
    result.add_argument("--pr", type=int, help="PR number; required in merge mode")
    result.add_argument("--fetch", action="store_true", help="fetch origin before checking refs")
    return result


def main() -> int:
    args = parser().parse_args()
    if args.mode == "merge" and args.pr is None:
        parser().error("merge mode requires --pr <number>")

    code, root_output = run(["git", "rev-parse", "--show-toplevel"])
    if code != 0 or not root_output:
        print(json.dumps({"schema_version": 1, "overall": "blocked", "error": "not_a_git_repository"}))
        return 1
    root = Path(root_output)
    os.chdir(root)

    branch = git_value(root, "branch", "--show-current")
    head_sha = git_value(root, "rev-parse", "HEAD")
    upstream = git_value(root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
    dirty = bool(git_value(root, "status", "--porcelain=v1"))

    fetch_status = "not_requested"
    if args.fetch:
        fetch_status = "success" if run(["git", "fetch", "--quiet", "--prune", "origin"], timeout=60, cwd=root)[0] == 0 else "failed"

    ahead = behind = 0
    sync_status = "not_applicable"
    if upstream:
        counts = git_value(root, "rev-list", "--left-right", "--count", f"HEAD...{upstream}")
        parts = counts.split()
        if len(parts) == 2 and all(part.isdigit() for part in parts):
            ahead, behind = map(int, parts)
            sync_status = "pass" if ahead == 0 and behind == 0 else "attention"
        else:
            sync_status = "unknown"

    git_state = "pass"
    if dirty or sync_status == "attention":
        git_state = "attention"
    elif fetch_status == "failed" or sync_status == "unknown":
        git_state = "unknown"

    github: dict[str, Any] = {
        "status": "unknown",
        "repository": None,
        "status_next_count": None,
        "t010_issue": None,
    }
    pr: dict[str, Any] = {
        "number": args.pr,
        "state": None,
        "head_sha": None,
        "mergeable": None,
        "review_decision": None,
        "url": None,
    }
    ci: dict[str, Any] = {
        "status": "not_applicable",
        "conclusion": None,
        "head_sha": None,
        "head_sha_matches": None,
        "url": None,
    }

    repo_ok, repo_data = gh_json(["repo", "view", "--json", "nameWithOwner,defaultBranchRef"])
    if repo_ok and isinstance(repo_data, dict):
        repository = repo_data.get("nameWithOwner")
        github["status"] = "pass"
        github["repository"] = repository

        next_ok, next_data = gh_json(
            ["issue", "list", "--state", "open", "--limit", "100", "--json", "number,title,url,labels"],
        )
        if next_ok and isinstance(next_data, list):
            github["status_next_count"] = sum(
                1
                for item in next_data
                if any(label.get("name") == "status:next" for label in item.get("labels", []))
            )
        else:
            github["status"] = "unknown"

        t010_ok, t010_data = gh_json(
            ["issue", "list", "--state", "all", "--search", "T-010 in:title", "--limit", "20", "--json", "number,title,state,url"],
        )
        if t010_ok and isinstance(t010_data, list):
            matches = [item for item in t010_data if "T-010" in str(item.get("title", "")).upper()]
            github["t010_issue"] = matches[0] if matches else None
        else:
            github["status"] = "unknown"

        if args.pr is not None:
            pr_ok, pr_data = gh_json(
                ["pr", "view", str(args.pr), "--json", "state,headRefOid,mergeable,reviewDecision,url"],
            )
            if pr_ok and isinstance(pr_data, dict):
                pr.update(
                    state=pr_data.get("state"),
                    head_sha=pr_data.get("headRefOid"),
                    mergeable=pr_data.get("mergeable"),
                    review_decision=pr_data.get("reviewDecision"),
                    url=pr_data.get("url"),
                )
                if pr["head_sha"]:
                    runs_ok, runs_data = gh_json(
                        [
                            "run",
                            "list",
                            "--commit",
                            str(pr["head_sha"]),
                            "--limit",
                            "20",
                            "--json",
                            "status,conclusion,headSha,url",
                        ],
                    )
                    matching_runs = [
                        item for item in (runs_data if isinstance(runs_data, list) else [])
                        if item.get("headSha") == pr["head_sha"]
                    ]
                    if matching_runs:
                        latest = matching_runs[0]
                        ci.update(
                            status="pass" if latest.get("conclusion") == "success" else "attention",
                            conclusion=latest.get("conclusion"),
                            head_sha=latest.get("headSha"),
                            head_sha_matches=latest.get("headSha") == pr["head_sha"],
                            url=latest.get("url"),
                        )
                    else:
                        ci["status"] = "unknown" if runs_ok else "unknown"
            else:
                github["status"] = "unknown"
    else:
        github["status"] = "unknown"

    overall = "pass"
    next_action = "continue"
    if git_state == "unknown" or github["status"] == "unknown":
        overall = "unknown"
        next_action = "resolve_status_check"
    elif args.mode == "merge" and git_state != "pass":
        overall = "blocked"
        next_action = "resolve_merge_gates"
    elif git_state == "attention":
        overall = "attention"
        next_action = "review_local_git_state"
    elif args.mode != "merge":
        has_next = github["status_next_count"]
        has_t010 = github["t010_issue"] is not None
        if has_next == 0 or not has_t010:
            overall = "attention"
            next_action = "create_or_refine_task_issue"
    elif (
        pr["state"] != "OPEN"
        or ci["conclusion"] != "success"
        or ci["head_sha_matches"] is not True
        or pr["mergeable"] != "MERGEABLE"
    ):
        overall = "blocked"
        next_action = "resolve_merge_gates"

    output = {
        "schema_version": 1,
        "mode": args.mode,
        "overall": overall,
        "git": {
            "branch": branch,
            "head_sha": head_sha,
            "upstream": upstream or None,
            "worktree_clean": not dirty,
            "ahead": ahead,
            "behind": behind,
            "sync_status": sync_status,
            "fetch_status": fetch_status,
        },
        "github": github,
        "pr": pr,
        "ci": ci,
        "next_action": next_action,
    }
    print(json.dumps(output, ensure_ascii=False, separators=(",", ":")))
    return 0 if overall in ("pass", "attention") else 1


if __name__ == "__main__":
    sys.exit(main())
