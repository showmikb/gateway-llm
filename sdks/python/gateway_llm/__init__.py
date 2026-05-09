"""Public surface of the gateway-llm Python SDK.

The SDK is deliberately additive on top of openai-python: any import of
`openai.OpenAI` still works against Gateway-LLM via base_url. This
package adds typed helpers for the extension headers Gateway-LLM
consumes (SmartRouter hints, recording tags) and for verifying the
Ed25519 receipts served at /.well-known/gateway-llm-receipts.json.
"""

from .client import GatewayLLM
from .receipts import Receipt, verify_receipt, load_public_key
from .hints import HEADER_TAGS, HEADER_QUALITY, HEADER_MAX_COST, HEADER_SPECULATIVE, HEADER_TRACE_ID

__all__ = [
    "GatewayLLM",
    "Receipt",
    "verify_receipt",
    "load_public_key",
    "HEADER_TAGS",
    "HEADER_QUALITY",
    "HEADER_MAX_COST",
    "HEADER_SPECULATIVE",
    "HEADER_TRACE_ID",
]

__version__ = "0.1.0"
