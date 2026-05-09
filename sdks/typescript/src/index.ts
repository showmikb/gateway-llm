// Official TypeScript SDK for Gateway-LLM.
//
// The SDK is a drop-in wrapper over `openai` (the official OpenAI
// Node client). Any existing `openai.OpenAI` call still works against
// a Gateway-LLM deployment via base_url. This package adds typed
// routing hints, recording metadata, and local receipt verification.

import OpenAI from "openai";

export const HEADERS = {
  TAGS:          "X-Gateway-LLM-Tags",
  QUALITY:       "X-Gateway-LLM-Quality",
  SPECULATIVE:   "X-Gateway-LLM-Speculative",
  MAX_COST:      "X-Gateway-LLM-Max-Cost-USD",
  TRACE_ID:      "X-Gateway-LLM-Trace-Id",
  RECORD:        "X-Gateway-LLM-Record",
  RECEIPT_ID:    "X-Gateway-LLM-Receipt-Id",
  SAVINGS_USD:   "X-Gateway-LLM-Estimated-Savings-USD",
  COMPLEXITY:    "X-Gateway-LLM-Complexity-Score",
  ROUTING:       "X-Gateway-LLM-Routing-Decision",
  PII:           "X-Gateway-LLM-Pii-Redactions",
} as const;

export interface GatewayHints {
  tags?: string[];
  quality?: "cheap" | "balanced" | "best";
  speculative?: boolean;
  maxCostUSD?: number;
  traceId?: string;
  record?: boolean;
}

export interface GatewayTelemetry {
  receiptId?: string;
  complexityScore?: number;
  routingDecision?: string;
  estimatedSavingsUSD?: number;
  piiRedactions?: number;
}

export type ChatCompletionWithTelemetry<T> = T & { gatewayLLM: GatewayTelemetry };

function toHeaders(h: GatewayHints): Record<string, string> {
  const out: Record<string, string> = {};
  if (h.tags?.length)            out[HEADERS.TAGS]        = h.tags.join(",");
  if (h.quality)                 out[HEADERS.QUALITY]     = h.quality;
  if (h.speculative !== undefined) out[HEADERS.SPECULATIVE] = String(h.speculative);
  if (h.maxCostUSD !== undefined)  out[HEADERS.MAX_COST]    = h.maxCostUSD.toFixed(6);
  if (h.traceId)                 out[HEADERS.TRACE_ID]    = h.traceId;
  if (h.record !== undefined)    out[HEADERS.RECORD]      = String(h.record);
  return out;
}

function fromResponseHeaders(h: Headers | Record<string, string>): GatewayTelemetry {
  const get = (k: string): string | undefined => {
    if (h instanceof Headers) return h.get(k) ?? undefined;
    return h[k] ?? h[k.toLowerCase()];
  };
  const complexity = get(HEADERS.COMPLEXITY);
  const savings = get(HEADERS.SAVINGS_USD);
  const pii = get(HEADERS.PII);
  return {
    receiptId:           get(HEADERS.RECEIPT_ID),
    complexityScore:     complexity ? Number(complexity) : undefined,
    routingDecision:     get(HEADERS.ROUTING),
    estimatedSavingsUSD: savings ? Number(savings) : undefined,
    piiRedactions:       pii ? Number(pii) : undefined,
  };
}

export interface GatewayLLMClientOptions {
  apiKey: string;
  baseURL: string;
  defaultHeaders?: Record<string, string>;
}

export class GatewayLLMClient {
  readonly openai: OpenAI;
  private readonly baseURL: string;
  private readonly apiKey: string;

  constructor(opts: GatewayLLMClientOptions) {
    this.baseURL = opts.baseURL.replace(/\/$/, "");
    this.apiKey = opts.apiKey;
    this.openai = new OpenAI({
      apiKey: opts.apiKey,
      baseURL: this.baseURL + "/v1",
      defaultHeaders: opts.defaultHeaders,
    });
  }

  /** Chat completion with Gateway-LLM hints. */
  async chatCompletions(
    params: OpenAI.Chat.ChatCompletionCreateParamsNonStreaming & { gateway?: GatewayHints }
  ): Promise<ChatCompletionWithTelemetry<OpenAI.Chat.ChatCompletion>> {
    const { gateway, ...rest } = params;
    const resp = await this.openai.chat.completions.create(rest, {
      headers: toHeaders(gateway ?? {}),
    }).withResponse();
    (resp.data as any).gatewayLLM = fromResponseHeaders(resp.response.headers);
    return resp.data as ChatCompletionWithTelemetry<OpenAI.Chat.ChatCompletion>;
  }

  /**
   * Fetch and locally verify a signed receipt. The caller is expected
   * to have installed the peer dep `@noble/ed25519`; if missing, the
   * returned object only carries the fetched envelope without the
   * cryptographic check.
   */
  async verifyReceipt(receiptId: string): Promise<{ valid: boolean; receipt: any }> {
    const headers: Record<string, string> = { Authorization: `Bearer ${this.apiKey}` };
    const resp = await fetch(`${this.baseURL}/v1/receipts/${receiptId}`, { headers });
    if (!resp.ok) throw new Error(`receipt fetch failed: ${resp.status}`);
    const receipt = await resp.json();

    let valid = false;
    try {
      const ed = await import("@noble/ed25519");
      const wk = await fetch(`${this.baseURL}/.well-known/gateway-llm-receipts.json`);
      if (!wk.ok) throw new Error(`well-known fetch: ${wk.status}`);
      const wkj = await wk.json();
      const pub = Uint8Array.from(Buffer.from(wkj.public_key, "base64"));
      const sig = Uint8Array.from(Buffer.from(receipt.sig, "base64"));
      const canonical = canonicalBytes(receipt);
      valid = await ed.verify(sig, canonical, pub);
    } catch {
      // No @noble/ed25519 installed: caller can verify elsewhere.
      valid = false;
    }
    return { valid, receipt };
  }
}

// Matches backend/internal/receipt/receipt.go:CanonicalBytes exactly.
function canonicalBytes(r: any): Uint8Array {
  const body: Record<string, unknown> = {
    id:             r.id,
    trace_id:       r.trace_id || undefined,
    org_id:         r.org_id || undefined,
    api_key_id:     r.api_key_id || undefined,
    alias:          r.alias,
    provider:       r.provider,
    provider_model: r.provider_model,
    region:         r.region || undefined,
    ts:             r.ts,
    req_hash:       r.req_hash,
    resp_hash:      r.resp_hash,
    prompt_tokens:      r.prompt_tokens ?? 0,
    completion_tokens:  r.completion_tokens ?? 0,
    total_tokens:       r.total_tokens ?? 0,
    cost_usd:           r.cost_usd ?? 0,
    prev_receipt_id:    r.prev_receipt_id || undefined,
    prev_hash:          r.prev_hash || undefined,
    public_key_id:      r.public_key_id,
  };
  Object.keys(body).forEach((k) => body[k] === undefined && delete body[k]);
  return new TextEncoder().encode(JSON.stringify(body));
}

export { toHeaders as gatewayHeaders };
