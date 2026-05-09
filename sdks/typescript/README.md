# @gateway-llm/sdk

Official TypeScript/Node SDK for [Gateway-LLM](https://gateway-llm.dev).
Wraps `openai` so existing code keeps working, and adds typed hints
for SmartRouter, recording metadata, and signed billing receipts.

## Install

```bash
npm i @gateway-llm/sdk openai
# optional, enables local receipt verification:
npm i @noble/ed25519
```

## Usage

```ts
import { GatewayLLMClient } from "@gateway-llm/sdk";

const client = new GatewayLLMClient({
  apiKey: process.env.GATEWAY_KEY!,
  baseURL: "https://gateway.yourdomain.com",
});

const r = await client.chatCompletions({
  model: "gpt-4o",
  messages: [{ role: "user", content: "hello" }],
  gateway: {
    tags: ["demo", "ts-sdk"],
    quality: "cheap",
    speculative: true,
    maxCostUSD: 0.002,
  },
});

console.log(r.choices[0].message.content);
console.log("savings:", r.gatewayLLM.estimatedSavingsUSD);
console.log("receipt:", r.gatewayLLM.receiptId);

if (r.gatewayLLM.receiptId) {
  const { valid } = await client.verifyReceipt(r.gatewayLLM.receiptId);
  console.log("receipt verified:", valid);
}
```

## LangChain.js

```ts
import { ChatOpenAI } from "@langchain/openai";
import { gatewayHeaders } from "@gateway-llm/sdk/langchain";

const llm = new ChatOpenAI({
  apiKey: process.env.GATEWAY_KEY,
  configuration: {
    baseURL: "https://gateway.yourdomain.com/v1",
    defaultHeaders: gatewayHeaders({
      tags: ["langchain-js"],
      quality: "cheap",
    }),
  },
});
```
