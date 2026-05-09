"""LangChain / LlamaIndex adapter helpers.

These frameworks already know how to talk to an OpenAI-compatible
endpoint via `base_url` + `default_headers`. Rather than fork their
chat model classes, we expose a header-builder so users can plug
Gateway-LLM hints into whatever model class they already use.
"""

from __future__ import annotations

from typing import Iterable, Optional

from .hints import (
    HEADER_MAX_COST,
    HEADER_QUALITY,
    HEADER_RECORD,
    HEADER_SPECULATIVE,
    HEADER_TAGS,
    HEADER_TRACE_ID,
)


def gateway_llm_headers(
    *,
    tags: Optional[Iterable[str]] = None,
    quality: Optional[str] = None,
    speculative: Optional[bool] = None,
    max_cost_usd: Optional[float] = None,
    trace_id: Optional[str] = None,
    record: Optional[bool] = None,
) -> dict[str, str]:
    """Return a header dict suitable for `default_headers=`."""
    out: dict[str, str] = {}
    if tags:
        out[HEADER_TAGS] = ",".join(tags)
    if quality:
        out[HEADER_QUALITY] = quality
    if speculative is not None:
        out[HEADER_SPECULATIVE] = "true" if speculative else "false"
    if max_cost_usd is not None:
        out[HEADER_MAX_COST] = f"{max_cost_usd:.6f}"
    if trace_id:
        out[HEADER_TRACE_ID] = trace_id
    if record is not None:
        out[HEADER_RECORD] = "true" if record else "false"
    return out
