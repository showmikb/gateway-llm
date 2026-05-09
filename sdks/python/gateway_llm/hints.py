"""Header constants exposed by the Gateway-LLM OpenAI ingress.

Using header-level hints keeps the SDK fully wire-compatible with
openai-python: any third-party LangChain/LlamaIndex/DSPy integration
that passes `default_headers` picks these up for free.
"""

HEADER_TAGS = "X-Gateway-LLM-Tags"
HEADER_QUALITY = "X-Gateway-LLM-Quality"
HEADER_MAX_COST = "X-Gateway-LLM-Max-Cost-USD"
HEADER_SPECULATIVE = "X-Gateway-LLM-Speculative"
HEADER_TRACE_ID = "X-Gateway-LLM-Trace-Id"
HEADER_RECORD = "X-Gateway-LLM-Record"

RESPONSE_HEADER_RECEIPT_ID = "X-Gateway-LLM-Receipt-Id"
RESPONSE_HEADER_RECEIPT_KEY_ID = "X-Gateway-LLM-Receipt-Key-Id"
RESPONSE_HEADER_COMPLEXITY_SCORE = "X-Gateway-LLM-Complexity-Score"
RESPONSE_HEADER_ROUTING_DECISION = "X-Gateway-LLM-Routing-Decision"
RESPONSE_HEADER_SAVINGS_USD = "X-Gateway-LLM-Estimated-Savings-USD"
RESPONSE_HEADER_PII_REDACTIONS = "X-Gateway-LLM-Pii-Redactions"
