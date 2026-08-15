#!/usr/bin/env python3
"""Replay the public, read-only Hyperliquid eligibility probe."""

from __future__ import annotations

import json
import random
import sys
import time
from datetime import datetime, timezone
from decimal import Decimal, InvalidOperation
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
ASSET_MAP = ROOT / "testdata/data-sources/hyperliquid/asset-map-v1.json"
FAILURE_POLICY = ROOT / "testdata/data-sources/hyperliquid/failure-policy-v1.json"
ENDPOINT = "https://api.hyperliquid.xyz/info"


def post(payload: dict, policy: dict) -> object:
    request = Request(
        ENDPOINT,
        data=json.dumps(payload, separators=(",", ":")).encode(),
        headers={"Content-Type": "application/json", "User-Agent": "stocks-marketing-public-probe/1"},
        method="POST",
    )
    for attempt in range(policy["max_attempts"]):
        try:
            with urlopen(request, timeout=policy["timeout_ms"] / 1000) as response:
                if response.status != 200:
                    raise RuntimeError(f"HTTP {response.status} for {payload['type']}")
                return json.load(response)
        except HTTPError as error:
            if error.code not in policy["retry_http_statuses"] or attempt + 1 == policy["max_attempts"]:
                raise
        except (URLError, TimeoutError):
            if attempt + 1 == policy["max_attempts"]:
                raise
        cap = min(policy["backoff_base_ms"] * 2**attempt, policy["backoff_cap_ms"])
        time.sleep(random.uniform(0, cap) / 1000)
    raise RuntimeError("unreachable retry state")


def positive_decimal(value: object) -> bool:
    try:
        return Decimal(str(value)) > 0
    except (InvalidOperation, TypeError):
        return False


def main() -> int:
    asset_map = json.loads(ASSET_MAP.read_text(encoding="utf-8"))
    rest_policy = json.loads(FAILURE_POLICY.read_text(encoding="utf-8"))["rest"]
    metadata, contexts = post({"type": "metaAndAssetCtxs", "dex": asset_map["dex"]}, rest_policy)
    capped = set(post({"type": "perpsAtOpenInterestCap", "dex": asset_map["dex"]}, rest_policy))
    universe = metadata.get("universe", [])
    if len(universe) != len(contexts):
        raise RuntimeError("metaAndAssetCtxs universe/context length mismatch")

    discovered = {item["name"]: (item, context) for item, context in zip(universe, contexts)}
    results = []
    for mapping in asset_map["mappings"]:
        asset = mapping["asset"]
        meta_context = discovered.get(asset)
        if meta_context is None:
            results.append(
                {
                    "asset": asset,
                    "analysis_eligible": False,
                    "new_entry_eligible": False,
                    "reasons": ["ASSET_NOT_DISCOVERED"],
                    "entry_reasons": [],
                }
            )
            continue

        meta, context = meta_context
        book = post({"type": "l2Book", "coin": asset}, rest_policy)
        levels = book.get("levels", []) if isinstance(book, dict) else []
        two_sided = len(levels) == 2 and bool(levels[0]) and bool(levels[1])
        reasons = []
        if meta.get("isDelisted", False):
            reasons.append("ASSET_DELISTED")
        if not positive_decimal(context.get("openInterest")):
            reasons.append("OPEN_INTEREST_ZERO")
        if context.get("markPx") is None or context.get("oraclePx") is None:
            reasons.append("MARK_OR_ORACLE_MISSING")
        if not two_sided:
            reasons.append("BOOK_EMPTY_OR_ONE_SIDED")
        entry_reasons = ["OPEN_INTEREST_CAP_REACHED"] if asset in capped else []

        results.append(
            {
                "asset": asset,
                "analysis_eligible": not reasons,
                "new_entry_eligible": not reasons and not entry_reasons,
                "reasons": reasons,
                "entry_reasons": entry_reasons,
                "is_delisted": meta.get("isDelisted", False),
                "open_interest": context.get("openInterest"),
                "mark_px": context.get("markPx"),
                "oracle_px": context.get("oraclePx"),
                "book_time_ms": book.get("time") if isinstance(book, dict) else None,
                "best_bid": levels[0][0].get("px") if two_sided else None,
                "best_ask": levels[1][0].get("px") if two_sided else None,
            }
        )

    print(
        json.dumps(
            {
                "probe_version": 1,
                "source": "hyperliquid",
                "captured_at": datetime.now(timezone.utc).isoformat(),
                "endpoint": ENDPOINT,
                "contains_secrets": False,
                "assets": results,
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (HTTPError, URLError, TimeoutError, RuntimeError, ValueError, KeyError) as error:
        print(f"hyperliquid probe failed: {error}", file=sys.stderr)
        raise SystemExit(1)
