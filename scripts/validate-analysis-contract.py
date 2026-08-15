#!/usr/bin/env python3
"""Validate the frozen global-analysis contract fixtures with the stdlib."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata" / "global-analysis" / "v1"
STATE_ORDER = ["WEAK", "IMPROVING", "CONFIRMED", "STRONG", "PERSISTENT_STRONG"]


class ValidationError(Exception):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValidationError(message)


def load_json(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise ValidationError(f"无法读取有效 JSON：{path.relative_to(ROOT)}: {error}") from error


def walk(value: Any):
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def validate_report() -> None:
    report = load_json(FIXTURES / "example-report.json")
    evidence_ids = [item["evidence_id"] for item in report["evidence"]]
    observation_ids = [item["observation_id"] for item in report["observations"]]
    require(len(evidence_ids) == len(set(evidence_ids)), "报告存在重复 evidence_id")
    require(len(observation_ids) == len(set(observation_ids)), "报告存在重复 observation_id")

    evidence_set = set(evidence_ids)
    observation_set = set(observation_ids)
    for item in walk(report):
        if not isinstance(item, dict):
            continue
        for evidence_id in item.get("evidence_refs", []):
            require(evidence_id in evidence_set, f"未知 evidence_ref：{evidence_id}")
        for key in ("observation_refs", "input_observation_refs"):
            for observation_id in item.get(key, []):
                require(observation_id in observation_set, f"未知 {key}：{observation_id}")

    for name, score in report["scores"].items():
        component_total = round(sum(component["value"] for component in score["components"]), 1)
        require(component_total == score["value"], f"{name} 组件合计与 Score 不一致")

    positions = [step["position"] for step in report["confirmation_chain"]["steps"]]
    require(positions == [1, 2, 3, 4, 5, 6], "确认链必须按 1–6 排列")

    macro = [item for item in report["evidence"] if item["category"] == "MACRO_EVENT"]
    require(macro, "样例报告必须包含宏观证据")
    for item in macro:
        require("numeric_context" in item, f"宏观证据缺少 numeric_context：{item['evidence_id']}")
        context = item["numeric_context"]
        for key in ("actual", "consensus", "previous", "revision"):
            require(key in context, f"宏观证据缺少 {key}：{item['evidence_id']}")

    derived = [item for item in report["evidence"] if item["source"] == "domain-engine"]
    require(derived, "样例报告必须包含领域引擎派生 Evidence")
    require(all(item["source_tier"] == "DERIVED" for item in derived), "领域引擎派生 Evidence 必须使用 DERIVED")


def rs_state(value: float) -> str:
    if value >= 1.0:
        return "STRONG"
    if value >= 0.25:
        return "POSITIVE"
    if value <= -0.25:
        return "WEAK"
    return "NEUTRAL"


def validate_relative_strength_boundaries() -> None:
    cases = load_json(FIXTURES / "relative-strength-boundaries.json")["cases"]
    for case in cases:
        actual = rs_state(float(case["value_pp"]))
        require(actual == case["expected"], f"RS 边界失败：{case['value_pp']} 得到 {actual}")


def validate_scenarios() -> None:
    scenarios = load_json(FIXTURES / "scenarios.json")["scenarios"]
    names = {scenario["name"] for scenario in scenarios}
    expected_names = {
        "bullish_catalyst_accepted",
        "bullish_catalyst_rejected",
        "cross_market_bullish_confirmation",
        "cross_market_divergence_and_catalyst_rejection",
        "healthy_pullback_improves_entry_without_changing_trend",
        "trend_strengthens_but_chasing_is_worse",
        "missing_foreign_flow_reduces_confidence_not_score",
        "equal_priority_price_conflict_blocks_indicator",
        "non_overlapping_sessions_are_explicit",
    }
    require(names == expected_names, "场景集合不完整或包含未约定场景")

    by_name = {scenario["name"]: scenario for scenario in scenarios}
    require(by_name["cross_market_bullish_confirmation"]["expected"]["cross_market"] == "CONFIRMED", "正向链必须确认")
    divergence = by_name["cross_market_divergence_and_catalyst_rejection"]["expected"]
    require(divergence["cross_market"] == "DIVERGENCE", "背离场景必须输出 DIVERGENCE")
    require(divergence["catalyst"] == "REJECTED", "背离与外资净卖场景必须拒绝 Catalyst")
    chasing = by_name["trend_strengthens_but_chasing_is_worse"]["expected"]
    require(chasing["trend_direction"] == "UP", "暴涨场景 Trend 应上升")
    require(chasing["entry_direction"] == "DOWN", "暴涨场景 Entry 应下降")
    require(by_name["healthy_pullback_improves_entry_without_changing_trend"]["expected"]["entry_direction"] == "UP", "健康回踩 Entry 应上升")
    missing = by_name["missing_foreign_flow_reduces_confidence_not_score"]["expected"]
    require(missing["foreign_flow_direction"] is None, "缺失资金流不得猜测")
    require(missing["trend_score_available"] is True, "缺失资金流不能自动令 Score 不可用")
    conflict = by_name["equal_priority_price_conflict_blocks_indicator"]["expected"]
    require(conflict["indicator_availability"] == "DATA_CONFLICT", "同级来源冲突必须显式标记")
    require(conflict["indicator_state"] is None, "冲突指标不得输出状态")
    require(by_name["bullish_catalyst_accepted"]["expected"]["catalyst"] == "ACCEPTED", "接受场景状态错误")
    require(by_name["bullish_catalyst_rejected"]["expected"]["catalyst"] == "REJECTED", "拒绝场景状态错误")
    asynchronous = by_name["non_overlapping_sessions_are_explicit"]["expected"]
    require(asynchronous["must_report_both_session_dates"] is True, "跨市场分析必须保留双方 session date")
    require(asynchronous["must_not_claim_simultaneous_quotes"] is True, "非重叠时段不得声称报价同步")


def validate_memory_transitions() -> None:
    cases = load_json(FIXTURES / "memory-state-transitions.json")["cases"]
    for case in cases:
        source = STATE_ORDER.index(case["input"]["state"])
        target = STATE_ORDER.index(case["expected"]["state"])
        require(abs(target - source) <= 1, f"Memory 状态单日跨越多级：{case['name']}")


def validate_markdown_links() -> None:
    for path in (ROOT / "docs").glob("*.md"):
        text = path.read_text(encoding="utf-8")
        for target in re.findall(r"\[[^]]+\]\(([^)]+)\)", text):
            if target.startswith(("http://", "https://", "#")):
                continue
            resolved = (path.parent / target.split("#", 1)[0]).resolve()
            require(resolved.exists(), f"失效 Markdown 链接：{path.relative_to(ROOT)} -> {target}")

        previous_level = 0
        for line in text.splitlines():
            match = re.match(r"^(#{1,6})\s+", line)
            if not match:
                continue
            level = len(match.group(1))
            require(previous_level == 0 or level <= previous_level + 1, f"标题层级跳跃：{path.relative_to(ROOT)}: {line}")
            previous_level = level


def main() -> int:
    try:
        validate_report()
        validate_relative_strength_boundaries()
        validate_scenarios()
        validate_memory_transitions()
        validate_markdown_links()
    except ValidationError as error:
        print(f"analysis contract validation failed: {error}", file=sys.stderr)
        return 1
    print("analysis contract semantic checks passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
