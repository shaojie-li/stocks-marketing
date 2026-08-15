#!/usr/bin/env python3
"""Run guarded, deterministic Git/worktree delivery operations."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path


def run(command: list[str], cwd: Path, check: bool = True) -> str:
    result = subprocess.run(command, cwd=cwd, text=True, capture_output=True)
    if check and result.returncode:
        detail = result.stderr.strip() or result.stdout.strip() or "命令失败"
        raise RuntimeError(f"{' '.join(command)}: {detail}")
    return result.stdout.strip()


def git(repo: Path, *args: str, check: bool = True) -> str:
    return run(["git", *args], repo, check)


def preflight(repo: Path) -> int:
    git(repo, "fetch", "origin", "main")
    base = git(repo, "rev-parse", "origin/main")
    worktrees = git(repo, "worktree", "list", "--porcelain")
    print(json.dumps({"repo": str(repo), "origin_main": base, "worktrees": worktrees}, ensure_ascii=False, indent=2))
    return 0


def create(repo: Path, path: Path, branch: str) -> int:
    if path.exists():
        raise RuntimeError(f"worktree 路径已存在：{path}")
    if git(repo, "show-ref", "--verify", f"refs/heads/{branch}", check=False):
        raise RuntimeError(f"本地分支已存在：{branch}")
    base = git(repo, "rev-parse", "origin/main")
    path.parent.mkdir(parents=True, exist_ok=True)
    git(repo, "worktree", "add", "-b", branch, str(path), "origin/main")
    actual = git(path, "rev-parse", "HEAD")
    if actual != base:
        raise RuntimeError(f"新 worktree 未基于 origin/main：{actual} != {base}")
    print(json.dumps({"branch": branch, "path": str(path), "base": base}, ensure_ascii=False, indent=2))
    return 0


def verify(repo: Path, path: Path) -> int:
    expected = git(repo, "rev-parse", "origin/main")
    actual = git(path, "rev-parse", "HEAD")
    branch = git(path, "branch", "--show-current")
    if actual != expected:
        raise RuntimeError(f"worktree HEAD 与 origin/main 不一致：{actual} != {expected}")
    print(json.dumps({"branch": branch, "path": str(path), "head": actual, "matches_origin_main": True}, ensure_ascii=False, indent=2))
    return 0


def commit(repo: Path, message: str, files: list[str]) -> int:
    if not files:
        raise RuntimeError("至少指定一个待提交文件")
    run(["git", "diff", "--check"], repo)
    run(["git", "add", "--", *files], repo)
    run(["git", "diff", "--cached", "--check"], repo)
    run(["git", "commit", "-m", message], repo)
    print(json.dumps({"branch": git(repo, "branch", "--show-current"), "commit": git(repo, "rev-parse", "HEAD")}, ensure_ascii=False, indent=2))
    return 0


def push(repo: Path, branch: str) -> int:
    current = git(repo, "branch", "--show-current")
    if current != branch:
        raise RuntimeError(f"当前分支不是指定分支：{current} != {branch}")
    run(["git", "push", "--set-upstream", "origin", branch], repo)
    print(json.dumps({"branch": branch, "pushed": True}, ensure_ascii=False, indent=2))
    return 0


def gh(args: list[str], repo: Path) -> str:
    return run(["gh", *args], repo)


def merge_and_cleanup(repo: Path, path: Path, branch: str, pr: str) -> int:
    details = json.loads(gh(["pr", "view", pr, "--json", "state,baseRefName,headRefName,headRefOid,mergeable,url"], repo))
    if details["state"] not in ("OPEN", "MERGED"):
        raise RuntimeError(f"PR 不是 open 状态：{details['state']}")
    if details["baseRefName"] != "main" or details["headRefName"] != branch:
        raise RuntimeError("PR 的 base/head 与指定目标不一致")
    if details["state"] == "OPEN":
        local_head = git(path, "rev-parse", "HEAD")
        if details["headRefOid"] != local_head:
            raise RuntimeError(f"PR head SHA 与 worktree HEAD 不一致：{details['headRefOid']} != {local_head}")
        checks = subprocess.run(["gh", "pr", "checks", pr, "--json", "name,state,bucket,workflow"], cwd=repo, text=True, capture_output=True)
        if checks.returncode:
            raise RuntimeError("必需 CI 尚未全部通过或无法读取：" + (checks.stderr.strip() or checks.stdout.strip()))
        reported_checks = json.loads(checks.stdout)
        if not reported_checks:
            raise RuntimeError("PR 没有可核验的 CI 检查")
        failed_checks = [check for check in reported_checks if check.get("bucket") != "pass" or check.get("state") != "SUCCESS"]
        if failed_checks:
            raise RuntimeError("CI 尚未全部通过：" + json.dumps(failed_checks, ensure_ascii=False))
        gh(["pr", "merge", pr, "--squash"], repo)
    merged = json.loads(gh(["pr", "view", pr, "--json", "state,mergeCommit,url"], repo))
    if merged["state"] != "MERGED":
        raise RuntimeError(f"PR 合并结果未确认：{merged['state']}")
    if not path.exists():
        raise RuntimeError(f"worktree 路径不存在，无法确认清理目标：{path}")
    if git(path, "branch", "--show-current") != branch:
        raise RuntimeError("worktree 当前分支与指定分支不一致")
    if git(path, "status", "--porcelain"):
        raise RuntimeError("合并后 worktree 仍有未提交修改，拒绝删除")
    if git(repo, "ls-remote", "--heads", "origin", branch):
        git(repo, "push", "origin", "--delete", branch)
    git(repo, "worktree", "remove", str(path))
    git(repo, "branch", "-D", branch)
    print(json.dumps({"pr": pr, "url": merged["url"], "merge_commit": merged["mergeCommit"], "cleaned": True}, ensure_ascii=False, indent=2))
    return 0


def create_pr(repo: Path, branch: str, title: str, body_file: Path) -> int:
    current = git(repo, "branch", "--show-current")
    if current != branch:
        raise RuntimeError(f"当前分支不是指定分支：{current} != {branch}")
    url = gh(["pr", "create", "--base", "main", "--head", branch, "--title", title, "--body-file", str(body_file)], repo)
    print(json.dumps({"url": url.strip(), "branch": branch}, ensure_ascii=False, indent=2))
    return 0


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    sub = result.add_subparsers(dest="command", required=True)
    child = sub.add_parser("preflight")
    child.add_argument("--repo", type=Path, required=True)
    child.set_defaults(func=lambda args: preflight(args.repo))
    child = sub.add_parser("create")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--path", type=Path, required=True)
    child.add_argument("--branch", required=True)
    child.set_defaults(func=lambda args: create(args.repo, args.path, args.branch))
    child = sub.add_parser("verify")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--path", type=Path, required=True)
    child.set_defaults(func=lambda args: verify(args.repo, args.path))
    child = sub.add_parser("commit")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--message", required=True)
    child.add_argument("--file", action="append", default=[])
    child.set_defaults(func=lambda args: commit(args.repo, args.message, args.file))
    child = sub.add_parser("push")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--branch", required=True)
    child.set_defaults(func=lambda args: push(args.repo, args.branch))
    child = sub.add_parser("create-pr")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--branch", required=True)
    child.add_argument("--title", required=True)
    child.add_argument("--body-file", type=Path, required=True)
    child.set_defaults(func=lambda args: create_pr(args.repo, args.branch, args.title, args.body_file))
    child = sub.add_parser("merge-and-cleanup")
    child.add_argument("--repo", type=Path, required=True)
    child.add_argument("--path", type=Path, required=True)
    child.add_argument("--branch", required=True)
    child.add_argument("--pr", required=True)
    child.set_defaults(func=lambda args: merge_and_cleanup(args.repo, args.path, args.branch, args.pr))
    return result


if __name__ == "__main__":
    try:
        arguments = parser().parse_args()
        raise SystemExit(arguments.func(arguments))
    except (RuntimeError, OSError, json.JSONDecodeError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        raise SystemExit(1)
