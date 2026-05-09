# gateway-llm (Python)

The official Python client for [Gateway-LLM](https://gateway-llm.dev).
It is a thin, wire-compatible wrapper on top of `openai-python`: every
existing call keeps working, and the Gateway-LLM-specific goodies
(SmartRouter hints, recording tags, receipt verification) are exposed
as simple keyword arguments.

## Why use this instead of plain `openai`?

If you already use `openai-python` and the Gateway-LLM `base_url`,
nothing forces you to switch. This package adds:

* **SmartRouter hints** — `quality="cheap"`, `speculative=True`,
  `max_cost_usd=0.002`, etc.
* **Recording/eval metadata** — `tags=["retrieval","fast-path"]`,
  `trace_id=...`, `experiment="exp-42"`.
* **Typed receipts** — every response carries
  `response.gateway_llm.receipt_id`; call
  `gateway_llm.verify_receipt(receipt_id)` to get a signed envelope.
* **Savings telemetry** — `response.gateway_llm.estimated_savings_usd`.

## Install

```bash
pip install gateway-llm
```

## Quickstart

```python
from gateway_llm import GatewayLLM

client = GatewayLLM(
    api_key="sk-...your-gateway-llm-key...",
    base_url="https://gateway.yourdomain.com",  # self-hosted
)

resp = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "hello"}],
    # Gateway-LLM extensions:
    tags=["canary", "debug"],
    quality="cheap",          # ask SmartRouter to prefer a cheaper tier
    speculative=True,         # race cheap+expensive
    max_cost_usd=0.002,       # hard cap
)

print(resp.choices[0].message.content)
print("saved:", resp.gateway_llm.estimated_savings_usd)
print("receipt:", resp.gateway_llm.receipt_id)
```

## Receipts

```python
signed = client.verify_receipt(resp.gateway_llm.receipt_id)
assert signed.valid
```

## LangChain

```python
from langchain_openai import ChatOpenAI
from gateway_llm.langchain import gateway_llm_headers

llm = ChatOpenAI(
    base_url="https://gateway.yourdomain.com/v1",
    api_key="sk-...",
    default_headers=gateway_llm_headers(
        tags=["langchain", "summarize"],
        quality="cheap",
    ),
)
```

No custom chain class required: Gateway-LLM's OpenAI ingress understands
the headers natively.
