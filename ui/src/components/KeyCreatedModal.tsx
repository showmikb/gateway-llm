'use client';

import { useState } from 'react';
import { CopyButton } from '@/components/ui';

interface Props {
  rawKey: string;
  modelAlias?: string;
  onClose: () => void;
}

type Tab = 'curl' | 'python';

export function KeyCreatedModal({ rawKey, modelAlias, onClose }: Props) {
  const [tab, setTab] = useState<Tab>('curl');
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const endpoint = `${apiUrl}/v1`;
  const alias = modelAlias?.trim() || 'gpt-4o-mini';

  const curlSnippet = `curl ${endpoint}/chat/completions \\
  -H "Authorization: Bearer ${rawKey}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${alias}",
    "messages": [{"role": "user", "content": "Hello, Gateway-LLM!"}]
  }'`;

  const pythonSnippet = `from openai import OpenAI

client = OpenAI(
    api_key="${rawKey}",
    base_url="${endpoint}",
)

resp = client.chat.completions.create(
    model="${alias}",
    messages=[{"role": "user", "content": "Hello, Gateway-LLM!"}],
)
print(resp.choices[0].message.content)`;

  const snippet = tab === 'curl' ? curlSnippet : pythonSnippet;

  const tabCls = (t: Tab) =>
    `rounded-md px-3 py-1 text-xs font-medium transition-colors ${
      tab === t
        ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/40'
        : 'text-zinc-400 hover:text-zinc-200'
    }`;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4 backdrop-blur-sm"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-labelledby="key-created-title"
    >
      <div
        className="w-full max-w-2xl rounded-xl border border-zinc-700 bg-zinc-900 p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <div className="inline-flex items-center gap-2 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-0.5 text-[11px] font-medium text-emerald-300">
              <span className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-400" />
              Key created
            </div>
            <h2 id="key-created-title" className="mt-2 text-lg font-semibold text-white">
              Copy your key and send your first request
            </h2>
            <p className="mt-1 text-sm text-zinc-400">
              The raw key is shown once. Paste it into your app or try the snippet below.
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-zinc-500 hover:text-zinc-300"
            aria-label="Close"
          >
            &times;
          </button>
        </div>

        <div className="mt-5">
          <p className="mb-1 text-xs font-medium uppercase tracking-wide text-zinc-500">Your API key</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 break-all rounded-md border border-emerald-900/50 bg-zinc-950 px-3 py-2 font-mono text-sm text-emerald-200">
              {rawKey}
            </code>
            <CopyButton value={rawKey} label="Copy key" />
          </div>
        </div>

        <div className="mt-5">
          <div className="mb-2 flex items-center justify-between">
            <div className="inline-flex gap-1 rounded-lg border border-zinc-800 bg-zinc-950/40 p-1">
              <button type="button" className={tabCls('curl')} onClick={() => setTab('curl')}>
                curl
              </button>
              <button type="button" className={tabCls('python')} onClick={() => setTab('python')}>
                Python (OpenAI SDK)
              </button>
            </div>
            <CopyButton value={snippet} label="Copy snippet" />
          </div>
          <pre className="overflow-x-auto rounded-md border border-zinc-800 bg-zinc-950 p-4 font-mono text-xs leading-relaxed text-zinc-200">
            {snippet}
          </pre>
          <p className="mt-2 text-xs text-zinc-500">
            Endpoint: <code className="text-emerald-300">{endpoint}</code>
            {modelAlias ? null : (
              <span className="ml-2 text-zinc-500">
                (No routes yet? Create one on <span className="text-zinc-300">Model Routes</span> first so
                <code className="ml-1 text-zinc-300">{alias}</code> resolves.)
              </span>
            )}
          </p>
        </div>

        <div className="mt-6 flex items-center justify-end gap-3">
          <button type="button" className="gatewayllm-btn-secondary text-sm" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
