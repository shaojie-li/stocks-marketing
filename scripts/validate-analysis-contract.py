#!/usr/bin/env python3
"""Validate the frozen global-analysis contract fixtures with the stdlib."""

from __future__ import annotations

import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata" / "global-analysis" / "v1"
DATA_SOURCE_FIXTURES = ROOT / "testdata" / "data-sources" / "hyperliquid"
DART_FIXTURES = ROOT / "testdata" / "data-sources" / "dart"
OPENDART_FIXTURES = ROOT / "testdata" / "data-sources" / "opendart"
STATE_ORDER = ["WEAK", "IMPROVING", "CONFIRMED", "STRONG", "PERSISTENT_STRONG"]
RULE_VERSION = "global-analysis/1.7.0"


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
    require(report["rule_version"] == RULE_VERSION, "报告规则版本不是当前冻结版本")
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
    require(report["confirmation_chain"]["status"] == "COMPLETE" and report["confirmation_chain"]["first_break"] is None, "正式样例确认链必须完整")
    require(all(step["status"] == "PASS" and step["evidence_refs"] for step in report["confirmation_chain"]["steps"]), "正式样例确认链每步必须 PASS 且有证据")

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

    expected_pairs = {
        "memory_relative_strength": ("xyz:MU", "xyz:SMH"),
        "semiconductor_relative_strength": ("xyz:SMH", "xyz:XYZ100"),
        "ai_compute_rotation": ("xyz:AMD", "xyz:NVDA"),
        "skhy_sector_alpha": ("xyz:SKHY", "xyz:SMSN"),
        "skhy_market_alpha": ("xyz:SKHY", "xyz:KR200"),
    }
    for name, (left, right) in expected_pairs.items():
        indicator = report["indicators"][name]
        require((indicator["left_symbol"], indicator["right_symbol"]) == (left, right), f"{name} 使用了错误资产映射")

    core_symbols = {symbol for pair in expected_pairs.values() for symbol in pair}
    core_observations = [item for item in report["observations"] if item["symbol"] in core_symbols]
    require({item["symbol"] for item in core_observations} == core_symbols, "样例报告缺少核心合约 Observation")
    for observation in core_observations:
        require(observation["source"] == "fixture-hyperliquid", f"核心行情不是 Hyperliquid：{observation['symbol']}")
        require(observation["window_type"] == "CONTRACT_24H", f"核心行情窗口错误：{observation['symbol']}")
        require(observation["market_status"] == "CONTINUOUS", f"核心合约市场状态错误：{observation['symbol']}")


def validate_degraded_shadow_report() -> None:
    report = load_json(FIXTURES / "degraded-shadow-report.json")
    require(report["rule_version"] == RULE_VERSION, "降级 shadow 报告规则版本错误")
    require(report["model"] == "NOT_INVOKED", "只读 shadow 报告不得伪造模型调用")
    require(report["scores"]["fundamental"]["value"] is None, "降级 Fundamental 必须为 null")
    require(report["scores"]["entry"]["value"] is None, "降级 Entry 必须为 null")
    require(report["scores"]["entry"]["coverage_pct"] < 70, "降级 Entry 覆盖率必须低于门槛")
    require(report["strategy"]["current"] in {"OBSERVE", "WAIT"}, "降级策略必须观察或等待")
    require(report["strategy"]["best_structure"] == "NO_TRADE", "降级策略不得形成交易结构")
    require(report["strategy"]["invalidation_conditions"] == [], "NO_TRADE 不得伪造交易失效位")
    require(report["data_audit"]["confidence"] == "LOW", "降级报告 Confidence 必须为 LOW")
    require(report["confirmation_chain"]["first_break"] == 4, "真实确认链首个断点必须是 Foreign Flow")
    require(report["catalyst"]["expected_direction"] == "BEARISH", "真实 Catalyst 方向错误")
    require(report["catalyst"]["state"] == "REJECTED", "真实 Catalyst 接受状态错误")
    evidence_ids = {item["evidence_id"] for item in report["evidence"]}
    for item in walk(report):
        if isinstance(item, dict):
            for evidence_id in item.get("evidence_refs", []):
                require(evidence_id in evidence_ids, f"降级报告引用未知 Evidence：{evidence_id}")


def validate_asset_map() -> None:
    asset_map = load_json(DATA_SOURCE_FIXTURES / "asset-map-v1.json")
    require(asset_map["rule_version"] == RULE_VERSION, "资产映射与规则版本不一致")
    mappings = {item["semantic"]: item for item in asset_map["mappings"]}
    expected = {
        "MEMORY_EQUITY": "xyz:MU",
        "SEMICONDUCTOR_BENCHMARK": "xyz:SMH",
        "GROWTH_BENCHMARK": "xyz:XYZ100",
        "AMD_EQUITY": "xyz:AMD",
        "NVIDIA_EQUITY": "xyz:NVDA",
        "SK_HYNIX_EQUITY": "xyz:SKHY",
        "SAMSUNG_EQUITY": "xyz:SMSN",
        "KOREA_MARKET_BENCHMARK": "xyz:KR200",
        "SP500_BENCHMARK": "xyz:SP500",
        "USD_KRW": "xyz:KRW",
        "DOLLAR_INDEX": "xyz:DXY",
        "BRENT_CRUDE": "xyz:BRENTOIL",
        "WTI_CRUDE": "xyz:CL",
    }
    require({key: value["asset"] for key, value in mappings.items()} == expected, "资产语义映射不完整或不准确")
    require(all(item["asset"] != "xyz:SOXL" for item in asset_map["mappings"]), "SOXL 不得作为未变换的核心基准")
    rejected = {item["asset"] for item in asset_map["rejected_substitutions"]}
    require("xyz:SOXL" in rejected, "资产映射必须显式拒绝 SOXL 直接替代")


def validate_hyperliquid_failure_policy() -> None:
    policy = load_json(DATA_SOURCE_FIXTURES / "failure-policy-v1.json")
    rest = policy["rest"]
    websocket = policy["websocket"]
    gate = policy["eligibility_gate"]
    require(rest["timeout_ms"] == 15000, "公共探针超时必须固定为 15 秒")
    require(0 < rest["weight_budget_per_minute"] <= 600, "REST 权重预算必须保留至少 50% 官方额度")
    require(rest["max_attempts"] == 3, "REST 重试必须是有限的三次尝试")
    require(429 in rest["retry_http_statuses"], "429 必须进入有限退避")
    require(websocket["require_snapshot_after_reconnect"] is True, "WebSocket 重连后必须恢复快照")
    require(websocket["reconnect_backoff_ms"][-1] <= 30000, "WebSocket 重连退避上限不得超过 30 秒")
    required_rejections = {
        "ASSET_DELISTED",
        "OPEN_INTEREST_ZERO",
        "MARK_OR_ORACLE_MISSING",
        "BOOK_EMPTY_OR_ONE_SIDED",
        "TRANSPORT_STALE",
        "SNAPSHOT_RECOVERY_PENDING",
    }
    require(required_rejections <= set(gate["reject_when"]), "Eligibility Gate 缺少失败关闭条件")
    require(policy["terminal_behavior"] == "SKIPPED_SOURCE_INCOMPLETE", "数据失败必须停止分析")


def validate_hyperliquid_live_fixture() -> None:
    fixture = load_json(DATA_SOURCE_FIXTURES / "2026-08-15-public-market-check.json")
    require(fixture["source"] == "hyperliquid", "live fixture 来源错误")
    require(fixture["contains_secrets"] is False, "live fixture 不得包含凭据")
    coverage = fixture["coverage_snapshot"]
    required_available = {"MU", "SMH", "XYZ100", "AMD", "NVDA", "SKHY", "SMSN", "KR200", "SP500", "BRENTOIL", "CL"}
    require(required_available <= set(coverage["available_nonzero_open_interest"]), "live fixture 缺少可用映射证据")
    require({"KRW", "DXY"} <= set(coverage["delisted_zero_open_interest"]), "live fixture 缺少退市映射证据")
    require({"US2Y", "US10Y"} <= set(coverage["missing"]), "live fixture 未保留收益率缺失事实")
    for item in walk(fixture):
        if isinstance(item, dict):
            forbidden = {key.lower() for key in item} & {"token", "authorization", "account", "address"}
            require(not forbidden, f"live fixture 包含敏感字段：{sorted(forbidden)}")


def validate_skhy_daily_live_fixture() -> None:
    fixture = load_json(DATA_SOURCE_FIXTURES / "t008-skhy-daily-live-check.json")
    request = fixture["request"]["req"]
    require(request["coin"] == "xyz:SKHY" and request["interval"] == "1d", "SKHY 日线 live fixture 请求错误")
    require(fixture["completed_before_as_of"] < fixture["minimum_completed_bars"], "SKHY 日线 fixture 不再表达历史不足")
    require(fixture["availability"] == "UNAVAILABLE" and fixture["reason"] == "INSUFFICIENT_HISTORY", "SKHY 日线历史不足必须显式降级")
    require(len(fixture["response_sha256"]) == 64, "SKHY 日线 fixture 缺少响应哈希")
    require(fixture["last"]["T"] > request["endTime"], "SKHY 日线 fixture 必须证明形成中 candle 被排除")


def validate_crowding_live_fixture() -> None:
    fixture = load_json(DATA_SOURCE_FIXTURES / "t012-skhy-crowding-live-check.json")
    require(fixture["source"] == "hyperliquid" and fixture["symbol"] == "xyz:SKHY", "Crowding live fixture 来源错误")
    require(fixture["public_read_only"] is True and fixture["contains_secrets"] is False, "Crowding live fixture 安全边界错误")
    require(fixture["full_history_stored"] is False and fixture["raw_redistribution"] == "UNCONFIRMED", "Crowding live fixture 保存边界错误")
    require(fixture["funding_history"]["page_counts"] == [500, 220], "Crowding funding 分页证据错误")
    require(sum(fixture["funding_history"]["page_counts"]) == fixture["funding_history"]["required_samples"], "Crowding funding 覆盖不足")
    require(fixture["availability"] == "UNAVAILABLE" and fixture["available_inputs"] == 3, "Crowding 当前覆盖门错误")
    components = fixture["components"]
    require(components["open_interest"]["reason"] == "HISTORICAL_OI_UNAVAILABLE", "Crowding 不得伪造历史 OI")
    require(components["price_extension"]["completed_daily_bars"] < 50, "Crowding live fixture 不再表达日线历史不足")
    hashes = fixture["funding_history"]["response_sha256"] + [fixture["daily_response_sha256"], fixture["context_response_sha256"]]
    require(all(len(value) == 64 for value in hashes), "Crowding live fixture 缺少响应哈希")


def validate_entry_readiness_live_fixture() -> None:
    fixture = load_json(DATA_SOURCE_FIXTURES / "t014-entry-readiness-live-check.json")
    require(fixture["source"] == "hyperliquid" and fixture["symbol"] == "xyz:SKHY", "Entry readiness fixture 来源错误")
    require(fixture["public_read_only"] is True and fixture["contains_secrets"] is False, "Entry readiness fixture 安全边界错误")
    daily = fixture["daily"]
    require(daily["completed"] < daily["minimum_completed_bars"] and daily["forming_excluded"] == 1, "Entry readiness fixture 不再表达日线不足")
    book = fixture["book"]
    require(float(book["spread_pct"]) <= 0.30, "Entry readiness 合约价差超过冻结门槛")
    require(min(float(book["bid_depth_50bps_usd"]), float(book["ask_depth_50bps_usd"])) >= 100_000, "Entry readiness 合约深度不足")
    require(fixture["entry_availability"] == "UNAVAILABLE" and fixture["foreign_flow"] == "UNAVAILABLE", "Entry readiness 不得补齐缺失输入")
    require(all(len(value) == 64 for value in (daily["response_sha256"], book["response_sha256"])), "Entry readiness fixture 缺少响应哈希")
    require(not any(fixture[key] for key in ("llm_invoked", "discord_invoked", "trading_api_invoked")), "Entry readiness live check 调用了禁止接口")


def validate_dart_live_fixture() -> None:
    path = DART_FIXTURES / "t010-skhy-company-rss.xml"
    try:
        root = ET.parse(path).getroot()
    except (OSError, ET.ParseError) as error:
        raise ValidationError(f"无法读取 DART RSS fixture：{error}") from error
    namespaces = {"dc": "http://purl.org/dc/elements/1.1/"}
    items = root.findall("./channel/item")
    selected = [item for item in items if item.findtext("title") == "(유가)SK하이닉스 - 파생상품거래손실발생"]
    require(len(selected) == 1, "DART RSS fixture 必须包含唯一受支持披露")
    item = selected[0]
    link = item.findtext("link")
    require(link == "https://dart.fss.or.kr/api/link.jsp?rcpNo=20260814802986", "DART RSS fixture rcpNo 或来源错误")
    require(item.findtext("guid") == link, "DART RSS fixture guid 与 link 冲突")
    require(item.findtext("dc:creator", namespaces=namespaces) == "SK하이닉스", "DART RSS fixture 公司错误")
    require(item.findtext("dc:date", namespaces=namespaces) == "2026-08-14T07:44:00Z", "DART RSS fixture UTC 时间错误")


def validate_opendart_fundamental_live_fixture() -> None:
    fixture = load_json(OPENDART_FIXTURES / "t013-skhy-fundamental-live-check.json")
    require(fixture["source"] == "OpenDART official API", "Fundamental live fixture 来源错误")
    require(fixture["corp_code"] == "00164779" and fixture["target_symbol"] == "xyz:SKHY", "Fundamental live fixture 身份错误")
    report = fixture["report"]
    require(report["report_code"] == "11012" and report["fs_div"] == "CFS", "Fundamental 必须使用最终半年度 CFS")
    require(re.fullmatch(r"[0-9]{14}", report["receipt_no"]) is not None, "Fundamental receipt_no 无效")
    responses = fixture["responses"]
    hashes = [responses[key] for key in ("list_sha256", "financials_sha256", "prior_financials_sha256", "document_zip_sha256")]
    require(all(re.fullmatch(r"[0-9a-f]{64}", value) is not None for value in hashes), "Fundamental live fixture 缺少响应哈希")
    result = fixture["result"]
    require(result["availability"] == "AVAILABLE" and result["coverage_pct"] == 90, "Fundamental live fixture 覆盖门错误")
    require(result["available_components"] == 5 and result["unavailable_components"] == ["regulatory_customer_event_risk"], "Fundamental 不得补齐未证明风险")
    safety = fixture["safety"]
    require(not any(safety.values()), "Fundamental live check 不得记录密钥或调用写操作")


def validate_price_structure_boundaries() -> None:
    fixture = load_json(FIXTURES / "price-structure-boundaries.json")
    require(fixture["rule_version"] == RULE_VERSION, "Price Structure 向量规则版本错误")
    expected = {
        "ema_equality_is_above_support": "ABOVE_SUPPORT",
        "exact_broken_threshold_remains_range": "RANGE",
        "below_broken_threshold_is_broken": "BROKEN",
    }
    require({case["name"]: case["expected_state"] for case in fixture["cases"]} == expected, "Price Structure 边界向量不完整")


def validate_crowding_boundaries() -> None:
    fixture = load_json(FIXTURES / "crowding-boundaries.json")
    require(fixture["rule_version"] == RULE_VERSION, "Crowding 向量规则版本错误")
    expected = {
        "zero_triggers_is_low": "LOW",
        "one_trigger_is_normal": "NORMAL",
        "two_triggers_is_normal": "NORMAL",
        "three_triggers_is_high": "HIGH",
        "four_triggers_below_extreme_extension_is_high": "HIGH",
        "four_triggers_at_extreme_extension_is_extreme": "EXTREME",
        "five_triggers_is_extreme": "EXTREME",
    }
    require({case["name"]: case["expected_state"] for case in fixture["cases"]} == expected, "Crowding 边界向量不完整")


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
        validate_degraded_shadow_report()
        validate_asset_map()
        validate_hyperliquid_failure_policy()
        validate_hyperliquid_live_fixture()
        validate_skhy_daily_live_fixture()
        validate_crowding_live_fixture()
        validate_entry_readiness_live_fixture()
        validate_dart_live_fixture()
        validate_opendart_fundamental_live_fixture()
        validate_relative_strength_boundaries()
        validate_price_structure_boundaries()
        validate_crowding_boundaries()
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
