"""The `GatewayLLM` client.

Composition over inheritance: we hold an `openai.OpenAI` instance and
delegate every attribute access to it. `chat.completions.create` is
intercepted so we can:

  * translate the extension kwargs (tags, quality, etc.) into
    Gateway-LLM header hints;
  * collect the receipt id + savings telemetry from the response
    headers and attach a lightweight `gateway_llm` namespace to the
    returned object.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Iterable, Optional

import httpx

try:
    from openai import OpenAI  # openai>=1.40
except ImportError as exc:  # pragma: no cover
    raise RuntimeError("gateway-llm requires `openai>=1.40`") from exc

from .hints import (
    HEADER_MAX_COST,
    HEADER_QUALITY,
    HEADER_RECORD,
    HEADER_SPECULATIVE,
    HEADER_TAGS,
    HEADER_TRACE_ID,
    RESPONSE_HEADER_COMPLEXITY_SCORE,
    RESPONSE_HEADER_PII_REDACTIONS,
    RESPONSE_HEADER_RECEIPT_ID,
    RESPONSE_HEADER_RECEIPT_KEY_ID,
    RESPONSE_HEADER_ROUTING_DECISION,
    RESPONSE_HEADER_SAVINGS_USD,
)
from .receipts import Receipt, verify_receipt, load_public_key


@dataclass
class GatewayLLMTelemetry:
    """Extra data Gateway-LLM attaches to every response."""

    receipt_id: Optional[str] = None
    receipt_key_id: Optional[str] = None
    complexity_score: Optional[float] = None
    routing_decision: Optional[str] = None
    estimated_savings_usd: Optional[float] = None
    pii_redactions: Optional[int] = None

    @classmethod
    def from_headers(cls, headers: dict[str, str]) -> "GatewayLLMTelemetry":
        def g(key: str) -> Optional[str]:
            for k, v in headers.items():
                if k.lower() == key.lower():
                    return v
            return None
        cx = g(RESPONSE_HEADER_COMPLEXITY_SCORE)
        sv = g(RESPONSE_HEADER_SAVINGS_USD)
        px = g(RESPONSE_HEADER_PII_REDACTIONS)
        return cls(
            receipt_id=g(RESPONSE_HEADER_RECEIPT_ID),
            receipt_key_id=g(RESPONSE_HEADER_RECEIPT_KEY_ID),
            complexity_score=float(cx) if cx else None,
            routing_decision=g(RESPONSE_HEADER_ROUTING_DECISION),
            estimated_savings_usd=float(sv) if sv else None,
            pii_redactions=int(px) if px else None,
        )


class GatewayLLM:
    """Gateway-LLM Python client.

    Use it exactly like `openai.OpenAI`; every call is wire-compatible.
    Extension kwargs (`tags`, `quality`, `speculative`, `max_cost_usd`,
    `trace_id`, `record`) are moved into Gateway-LLM headers before the
    request is sent.
    """

    def __init__(
        self,
        api_key: str,
        base_url: str,
        default_headers: Optional[dict[str, str]] = None,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._default_headers = dict(default_headers or {})
        self._openai = OpenAI(
            api_key=api_key,
            base_url=self._base_url + "/v1",
            default_headers=self._default_headers,
            http_client=http_client,
        )
        self.chat = _Chat(self)

    # Passthrough for everything the user might reach for (embeddings,
    # images, etc). Keeps the SDK 1:1 with openai-python.
    def __getattr__(self, name: str) -> Any:
        return getattr(self._openai, name)

    def verify_receipt(self, receipt_id: str) -> Receipt:
        """Fetch a signed receipt and verify it locally.

        The public key is cached in-process after the first call.
        """
        with httpx.Client(base_url=self._base_url, headers=self._default_headers) as c:
            r = c.get(
                f"/v1/receipts/{receipt_id}",
                headers={"Authorization": f"Bearer {self._openai.api_key}"},
            )
            r.raise_for_status()
            rcpt = Receipt.model_validate_json(r.content)

            pk = load_public_key(self._base_url, http=c)
        verify_receipt(rcpt, pk)
        return rcpt


class _Chat:
    def __init__(self, parent: GatewayLLM) -> None:
        self.completions = _ChatCompletions(parent)


class _ChatCompletions:
    def __init__(self, parent: GatewayLLM) -> None:
        self._parent = parent

    def create(
        self,
        *,
        model: str,
        messages: Iterable[dict],
        tags: Optional[Iterable[str]] = None,
        quality: Optional[str] = None,
        speculative: Optional[bool] = None,
        max_cost_usd: Optional[float] = None,
        trace_id: Optional[str] = None,
        record: Optional[bool] = None,
        **openai_kwargs: Any,
    ) -> Any:
        """Like OpenAI's chat.completions.create but with Gateway-LLM hints."""
        extra_headers = dict(openai_kwargs.pop("extra_headers", {}) or {})
        if tags:
            extra_headers[HEADER_TAGS] = ",".join(tags)
        if quality:
            extra_headers[HEADER_QUALITY] = quality
        if speculative is not None:
            extra_headers[HEADER_SPECULATIVE] = "true" if speculative else "false"
        if max_cost_usd is not None:
            extra_headers[HEADER_MAX_COST] = f"{max_cost_usd:.6f}"
        if trace_id:
            extra_headers[HEADER_TRACE_ID] = trace_id
        if record is not None:
            extra_headers[HEADER_RECORD] = "true" if record else "false"

        raw = self._parent._openai.chat.completions.with_raw_response.create(
            model=model,
            messages=list(messages),
            extra_headers=extra_headers,
            **openai_kwargs,
        )
        completion = raw.parse()
        telemetry = GatewayLLMTelemetry.from_headers(dict(raw.headers))
        # Attach in a namespace so we do not clash with OpenAI's own fields.
        setattr(completion, "gateway_llm", telemetry)
        return completion
