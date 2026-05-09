"""Ed25519 receipt verification.

The Gateway-LLM server publishes the active public key at
`/.well-known/gateway-llm-receipts.json` and emits a signed envelope for
every completed inference. This module gives customers a tiny, dep-free
way to verify those envelopes in their own code — no gateway-llm
process required to audit a bill.
"""

from __future__ import annotations

import base64
import json
from typing import Optional

import httpx
from pydantic import BaseModel, Field

try:
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    from cryptography.exceptions import InvalidSignature
except ImportError as exc:  # pragma: no cover
    raise RuntimeError(
        "gateway-llm receipt verification requires `cryptography>=41`"
    ) from exc


_PUBKEY_CACHE: dict[str, Ed25519PublicKey] = {}


class Receipt(BaseModel):
    """The signed envelope returned by /v1/receipts/{id}."""

    id: str
    trace_id: Optional[str] = None
    org_id: Optional[str] = None
    api_key_id: Optional[str] = None
    alias: str
    provider: str
    provider_model: str
    region: Optional[str] = None
    ts: str
    req_hash: str
    resp_hash: str
    prompt_tokens: int = 0
    completion_tokens: int = Field(0, alias="completion_tokens")
    total_tokens: int = 0
    cost_usd: float = 0.0
    prev_receipt_id: Optional[str] = None
    prev_hash: Optional[str] = None
    public_key_id: str
    sig: str

    model_config = {"populate_by_name": True}


def load_public_key(base_url: str, http: Optional[httpx.Client] = None) -> Ed25519PublicKey:
    """Fetch and cache the gateway's Ed25519 public key."""
    if base_url in _PUBKEY_CACHE:
        return _PUBKEY_CACHE[base_url]
    client = http or httpx.Client(base_url=base_url)
    try:
        r = client.get("/.well-known/gateway-llm-receipts.json")
        r.raise_for_status()
        pub = base64.b64decode(r.json()["public_key"])
    finally:
        if http is None:
            client.close()
    key = Ed25519PublicKey.from_public_bytes(pub)
    _PUBKEY_CACHE[base_url] = key
    return key


def _canonical_bytes(r: Receipt) -> bytes:
    """Mirror backend/internal/receipt/receipt.go:CanonicalBytes.

    Field order and omit-empty rules must match the server exactly.
    """
    body = {
        "id":             r.id,
        "trace_id":       r.trace_id or None,
        "org_id":         r.org_id or None,
        "api_key_id":     r.api_key_id or None,
        "alias":          r.alias,
        "provider":       r.provider,
        "provider_model": r.provider_model,
        "region":         r.region or None,
        "ts":             r.ts,
        "req_hash":       r.req_hash,
        "resp_hash":      r.resp_hash,
        "prompt_tokens":      r.prompt_tokens,
        "completion_tokens":  r.completion_tokens,
        "total_tokens":       r.total_tokens,
        "cost_usd":           r.cost_usd,
        "prev_receipt_id":    r.prev_receipt_id or None,
        "prev_hash":          r.prev_hash or None,
        "public_key_id":      r.public_key_id,
    }
    # Emulate Go's `omitempty` for strings: drop keys whose value is None.
    body = {k: v for k, v in body.items() if v is not None}
    return json.dumps(body, separators=(",", ":")).encode()


def verify_receipt(receipt: Receipt, pub: Ed25519PublicKey) -> bool:
    """Raise on bad signature; return True on success."""
    sig = base64.b64decode(receipt.sig)
    try:
        pub.verify(sig, _canonical_bytes(receipt))
    except InvalidSignature as exc:
        raise ValueError("receipt signature invalid") from exc
    return True
