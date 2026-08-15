#!/usr/bin/env python3
"""Validate and create project-standard GitHub Issues through gh."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path


PLAN_TITLE = re.compile(r"^\[M\d+\]\[T-\d{3}[A-Z]?\] .+\S$")
SPECIAL_TITLES = {
    "bug": re.compile(r"^\[Bug\] .+\S$"),
    "security": re.compile(r"^\[Security\] .+\S$"),
    "ops": re.compile(r"^\[Ops\] .+\S$"),
}
REQUIRED_HEADINGS = (
    "## 目标和用户价值",
    "## 依赖及解除条件",
    "## 范围",
    "## 非目标",
    "## 可观察行为与验收标准",
    "## 测试与质量门禁",
    "## 失败、重试、幂等、过期数据、安全和审计",
    "## 完成记录",
)
TASK_ID = re.compile(r"\[T-(\d{3})([A-Z]?)\]")


def run_gh(args: list[str], repo: str | None) -> str:
    command = ["gh", *args]
    if repo:
        command.extend(["--repo", repo])
    result = subprocess.run(command, text=True, capture_output=True)
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip() or "gh 命令失败"
        raise RuntimeError(detail)
    return result.stdout


def read_file(path: str) -> str:
    return Path(path).read_text(encoding="utf-8")


def validate(title: str, body: str, kind: str) -> list[str]:
    errors: list[str] = []
    pattern = PLAN_TITLE if kind in ("plan", "design") else SPECIAL_TITLES[kind]
    if not pattern.fullmatch(title.strip()):
        expected = "[M#][T-xxx] 结果导向标题" if kind in ("plan", "design") else f"[{kind.capitalize()}] 结果导向标题"
        errors.append(f"标题不符合 {expected} 格式")
    for heading in REQUIRED_HEADINGS:
        if heading not in body:
            errors.append(f"正文缺少章节：{heading}")
    if "<" in body or ">" in body or "待补充" in body:
        errors.append("正文仍包含占位符，请填入实际内容")
    acceptance = body.split("## 可观察行为与验收标准", 1)
    if len(acceptance) != 2 or "- [ ]" not in acceptance[1].split("## 测试与质量门禁", 1)[0]:
        errors.append("验收标准必须至少包含一个可勾选条目")
    return errors


def local_task_ids(path: str | None) -> set[str]:
    if not path:
        return set()
    return {f"T-{number}{suffix}" for number, suffix in TASK_ID.findall(read_file(path))}


def inspect(repo: str | None, task_index_file: str | None) -> int:
    issues = json.loads(run_gh(["issue", "list", "--state", "all", "--limit", "1000", "--json", "number,title,state,labels"], repo))
    labels = json.loads(run_gh(["label", "list", "--limit", "100", "--json", "name"], repo))
    task_ids = {f"T-{number}{suffix}" for issue in issues for number, suffix in TASK_ID.findall(issue["title"])}
    task_ids.update(local_task_ids(task_index_file))
    next_issues = [issue for issue in issues if issue["state"] == "OPEN" and any(label["name"] == "status:next" for label in issue["labels"])]
    print(json.dumps({"task_ids": sorted(task_ids), "open_status_next": next_issues, "labels": sorted(label["name"] for label in labels)}, ensure_ascii=False, indent=2))
    return 0


def validate_command(args: argparse.Namespace) -> int:
    title = read_file(args.title_file).strip()
    body = read_file(args.body_file)
    errors = validate(title, body, args.kind)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Issue 草稿校验通过")
    return 0


def create(args: argparse.Namespace) -> int:
    title = read_file(args.title_file).strip()
    body = read_file(args.body_file)
    errors = validate(title, body, args.kind)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    issues = json.loads(run_gh(["issue", "list", "--state", "all", "--limit", "1000", "--json", "number,title,state,labels"], args.repo))
    if args.kind in ("plan", "design"):
        match = TASK_ID.search(title)
        assert match is not None
        task_id = f"T-{match.group(1)}{match.group(2)}"
        if task_id in local_task_ids(args.task_index_file) or any(task_id in issue["title"] for issue in issues):
            raise RuntimeError(f"任务编号已存在：{task_id}")
    if args.status_next:
        next_issues = [issue for issue in issues if issue["state"] == "OPEN" and any(label["name"] == "status:next" for label in issue["labels"])]
        if next_issues:
            raise RuntimeError(f"已有开放 status:next Issue：#{next_issues[0]['number']}")

    labels = list(dict.fromkeys(args.label + (["status:next"] if args.status_next else [])))
    available = {label["name"] for label in json.loads(run_gh(["label", "list", "--limit", "100", "--json", "name"], args.repo))}
    missing = sorted(set(labels) - available)
    if missing and (not args.compatibility or "status:next" in missing):
        raise RuntimeError(f"GitHub 缺少规范标签：{', '.join(missing)}")
    applied_labels = [label for label in labels if label in available]
    command = ["issue", "create", "--title", title, "--body-file", args.body_file]
    for label in applied_labels:
        command.extend(["--label", label])
    url = run_gh(command, args.repo).strip()
    print(json.dumps({"url": url, "applied_labels": applied_labels, "missing_labels": missing}, ensure_ascii=False))
    return 0


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    sub = result.add_subparsers(dest="command", required=True)
    for command in ("inspect",):
        child = sub.add_parser(command)
        child.add_argument("--repo")
        child.add_argument("--task-index-file")
        child.set_defaults(func=lambda args: inspect(args.repo, args.task_index_file))
    for command in ("validate", "create"):
        child = sub.add_parser(command)
        child.add_argument("--title-file", required=True)
        child.add_argument("--body-file", required=True)
        child.add_argument("--kind", choices=("plan", "design", "bug", "security", "ops"), required=True)
        child.add_argument("--task-index-file")
        if command == "create":
            child.add_argument("--repo")
            child.add_argument("--label", action="append", default=[])
            child.add_argument("--status-next", action="store_true")
            child.add_argument("--compatibility", action="store_true", help="允许缺失非 status:next 标签并报告遗漏")
            child.set_defaults(func=create)
        else:
            child.set_defaults(func=validate_command)
    return result


if __name__ == "__main__":
    try:
        arguments = parser().parse_args()
        raise SystemExit(arguments.func(arguments))
    except (RuntimeError, OSError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        raise SystemExit(1)
