'use client';

import Link from 'next/link';
import { useEffect, useMemo, useState } from 'react';
import { createCredential, createDeployment, createKey } from '@/lib/api';
import { isUserApiKey, type ProviderCredential, type DeploymentRow, type APIKey } from '@/lib/types';
import { notifySetupChanged } from '@/lib/setupState';
import { CopyButton } from '@/components/ui';

type StepId = 'provider' | 'route' | 'key' | 'try';

interface Props {
  credentials: ProviderCredential[];
  deployments: DeploymentRow[];
  keys: APIKey[];
  totalReq?: number;
  onRefresh: () => Promise<void>;
  onDismiss: () => void;
  onComplete: () => void;
}

interface StepState {
  id: StepId;
  title: string;
  description: string;
  done: boolean;
}

const PROVIDER_HINTS: Record<string, string> = {
  openai: 'Get one at https://platform.openai.com/api-keys',
  anthropic: 'Get one at https://console.anthropic.com/settings/keys',
  gemini: 'Get one at https://aistudio.google.com/app/apikey',
};

const PROVIDER_DEFAULT_MODEL: Record<string, { alias: string; provider_model: string }> = {
  openai: { alias: 'gpt-4o-mini', provider_model: 'gpt-4o-mini' },
  anthropic: { alias: 'claude-sonnet', provider_model: 'claude-sonnet-4-20250514' },
  gemini: { alias: 'gemini-flash', provider_model: 'gemini-2.0-flash' },
};

export function OnboardingWizard({
  credentials,
  deployments,
  keys,
  totalReq = 0,
  onRefresh,
  onDismiss,
  onComplete,
}: Props) {
  const steps: StepState[] = useMemo(
    () => [
      {
        id: 'provider',
        title: '1. Add a provider API key',
        description:
          'Bring your own OpenAI, Anthropic, or Gemini key. It is encrypted at rest and never shared across organizations.',
        done: credentials.length > 0,
      },
      {
        id: 'route',
        title: '2. Create a model route',
        description:
          'Map a friendly alias (like "gpt-4o-mini") to a provider + model. The gateway uses this alias in requests.',
        done: deployments.length > 0,
      },
      {
        id: 'key',
        title: '3. Generate a gateway API key',
        description:
          'Create a virtual API key your apps will use. You will see the raw key exactly once — save it.',
        done: keys.filter(isUserApiKey).length > 0,
      },
      {
        id: 'try',
        title: '4. Try it out',
        description:
          'Send your first request with curl or the OpenAI SDK pointed at your new endpoint. This step auto-completes after your first successful gateway call.',
        done: totalReq > 0,
      },
    ],
    [credentials, deployments, keys, totalReq],
  );

  const firstIncomplete = steps.find((s) => !s.done)?.id ?? 'try';
  const [active, setActive] = useState<StepId>(firstIncomplete);
  const [rawKey, setRawKey] = useState<string | null>(null);
  const [keyCopiedConfirmed, setKeyCopiedConfirmed] = useState(false);

  useEffect(() => {
    // Don't auto-advance away from the 'key' step while the freshly created
    // key is still being shown — the user needs time to copy it.
    if (rawKey && !keyCopiedConfirmed) return;
    setActive(firstIncomplete);
  }, [firstIncomplete, rawKey, keyCopiedConfirmed]);

  const completedCount = steps.filter((s) => s.done).length;

  return (
    <div className="gatewayllm-card p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-medium text-white">Welcome to Gateway-LLM</h2>
          <p className="mt-1 text-sm text-zinc-400">
            Let&apos;s get your account ready in four quick steps. You&apos;ll be routing traffic in under two minutes.
          </p>
        </div>
        <button
          type="button"
          onClick={onDismiss}
          className="text-xs text-zinc-500 hover:text-zinc-300"
        >
          Skip for now
        </button>
      </div>

      <div className="mt-5 flex items-center gap-2">
        {steps.map((s, idx) => (
          <div
            key={s.id}
            className={`h-1.5 flex-1 rounded-full ${
              s.done ? 'bg-emerald-500' : idx === steps.findIndex((x) => !x.done) ? 'bg-emerald-500/40' : 'bg-zinc-800'
            }`}
          />
        ))}
      </div>
      <p className="mt-2 text-xs text-zinc-500">
        {completedCount} of {steps.length} complete
      </p>

      {rawKey && !keyCopiedConfirmed && (
        <div className="mt-5 rounded-lg border border-emerald-500/40 bg-emerald-500/5 p-4">
          <div className="flex items-center justify-between gap-3">
            <p className="text-sm font-medium text-emerald-200">
              Your new gateway API key — copy it now, it won&apos;t be shown again.
            </p>
          </div>
          <div className="mt-3 flex items-center gap-2">
            <code className="flex-1 break-all rounded border border-emerald-900/50 bg-zinc-950 px-3 py-2 font-mono text-xs text-emerald-200">
              {rawKey}
            </code>
            <CopyButton value={rawKey} label="Copy key" />
          </div>
          <div className="mt-3 flex items-center justify-end gap-3">
            <button
              type="button"
              onClick={() => {
                setKeyCopiedConfirmed(true);
                setActive('try');
              }}
              className="gatewayllm-btn text-xs"
            >
              I&apos;ve copied it — continue
            </button>
          </div>
        </div>
      )}

      <div className="mt-6 space-y-3">
        {steps.map((step) => {
          const expanded = active === step.id;
          return (
            <div
              key={step.id}
              className={`rounded-lg border ${
                expanded ? 'border-emerald-500/40 bg-emerald-500/5' : 'border-zinc-800 bg-zinc-900/40'
              }`}
            >
              <button
                type="button"
                className="flex w-full items-center justify-between px-4 py-3 text-left"
                onClick={() => setActive(expanded ? step.id : step.id)}
              >
                <span className="flex items-center gap-3">
                  <StatusDot done={step.done} active={expanded} />
                  <span>
                    <span className={`block text-sm font-medium ${step.done ? 'text-zinc-300' : 'text-white'}`}>
                      {step.title}
                    </span>
                    <span className="block text-xs text-zinc-500">{step.description}</span>
                  </span>
                </span>
                <span className="text-xs text-zinc-500">{expanded ? 'Hide' : step.done ? 'Review' : 'Open'}</span>
              </button>
              {expanded && (
                <div className="border-t border-zinc-800 p-4">
                  {step.id === 'provider' && (
                    <AddProviderStep onDone={async () => { await onRefresh(); setActive('route'); }} />
                  )}
                  {step.id === 'route' && (
                    <AddRouteStep credentials={credentials} onDone={async () => { await onRefresh(); setActive('key'); }} />
                  )}
                  {step.id === 'key' && (
                    <GenerateKeyStep
                      alreadyRevealed={!!rawKey}
                      onCreated={async (key) => {
                        setRawKey(key);
                        await onRefresh();
                      }}
                    />
                  )}
                  {step.id === 'try' && (
                    <TryItStep deployments={deployments} rawKey={rawKey} onComplete={onComplete} />
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function StatusDot({ done, active }: { done: boolean; active: boolean }) {
  if (done) {
    return (
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-emerald-500/20 text-emerald-300">
        <svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" aria-hidden>
          <path d="M7.629 13.229l-3.24-3.24 1.415-1.414 1.825 1.826 4.588-4.588 1.414 1.414z" />
        </svg>
      </span>
    );
  }
  return (
    <span
      className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full border ${
        active ? 'border-emerald-400 text-emerald-300' : 'border-zinc-700 text-zinc-500'
      }`}
    >
      <span className={`h-2 w-2 rounded-full ${active ? 'bg-emerald-400' : 'bg-zinc-700'}`} />
    </span>
  );
}

function AddProviderStep({ onDone }: { onDone: () => Promise<void> }) {
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('openai');
  const [apiKey, setApiKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function save() {
    setErr(null);
    if (!name.trim() || !apiKey.trim()) {
      setErr('Name and API key are required.');
      return;
    }
    setSaving(true);
    try {
      await createCredential({ name: name.trim(), provider, api_key: apiKey.trim() });
      notifySetupChanged();
      await onDone();
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to save provider key');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Name</label>
          <input
            className="gatewayllm-input text-sm"
            placeholder="my-openai-key"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Provider</label>
          <select
            className="gatewayllm-input text-sm"
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
          >
            <option value="openai">OpenAI</option>
            <option value="anthropic">Anthropic</option>
            <option value="gemini">Google Gemini</option>
          </select>
        </div>
      </div>
      <div>
        <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">API Key</label>
        <input
          type="password"
          className="gatewayllm-input font-mono text-sm"
          placeholder={provider === 'openai' ? 'sk-...' : provider === 'anthropic' ? 'sk-ant-...' : 'AIza...'}
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
        />
        <p className="mt-1 text-xs text-zinc-500">{PROVIDER_HINTS[provider]}</p>
      </div>
      {err && <p className="text-sm text-red-400">{err}</p>}
      <div className="flex justify-end">
        <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={saving} onClick={save}>
          {saving ? 'Saving...' : 'Save provider key'}
        </button>
      </div>
    </div>
  );
}

function AddRouteStep({
  credentials,
  onDone,
}: {
  credentials: ProviderCredential[];
  onDone: () => Promise<void>;
}) {
  const [credId, setCredId] = useState<string>(credentials[0]?.id ?? '');
  const selected = credentials.find((c) => c.id === credId);
  const defaultRoute = selected ? PROVIDER_DEFAULT_MODEL[selected.provider] : undefined;
  const [alias, setAlias] = useState(defaultRoute?.alias ?? '');
  const [providerModel, setProviderModel] = useState(defaultRoute?.provider_model ?? '');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    if (selected) {
      const d = PROVIDER_DEFAULT_MODEL[selected.provider];
      if (d) {
        setAlias((a) => a || d.alias);
        setProviderModel((p) => p || d.provider_model);
      }
    }
  }, [selected]);

  async function save() {
    setErr(null);
    if (!selected || !alias.trim() || !providerModel.trim()) {
      setErr('Pick a provider key and fill out alias + model.');
      return;
    }
    setSaving(true);
    try {
      await createDeployment({
        model_alias: alias.trim(),
        provider: selected.provider,
        provider_model: providerModel.trim(),
        credential_id: selected.id,
        priority: 1,
      });
      notifySetupChanged();
      await onDone();
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to create route');
    } finally {
      setSaving(false);
    }
  }

  if (credentials.length === 0) {
    return (
      <p className="text-sm text-zinc-400">
        Add a provider key first, then come back here to create your first route.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <div>
        <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Use provider key</label>
        <select
          className="gatewayllm-input text-sm"
          value={credId}
          onChange={(e) => setCredId(e.target.value)}
        >
          {credentials.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name} ({c.provider})
            </option>
          ))}
        </select>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div>
          <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Alias (your app uses this)</label>
          <input
            className="gatewayllm-input text-sm"
            placeholder="gpt-4o-mini"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
          />
        </div>
        <div>
          <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Provider model</label>
          <input
            className="gatewayllm-input text-sm"
            placeholder="gpt-4o-mini"
            value={providerModel}
            onChange={(e) => setProviderModel(e.target.value)}
          />
        </div>
      </div>
      {err && <p className="text-sm text-red-400">{err}</p>}
      <div className="flex justify-end">
        <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={saving} onClick={save}>
          {saving ? 'Creating...' : 'Create route'}
        </button>
      </div>
    </div>
  );
}

function GenerateKeyStep({
  alreadyRevealed,
  onCreated,
}: {
  alreadyRevealed: boolean;
  onCreated: (rawKey: string) => Promise<void>;
}) {
  const [name, setName] = useState('my-app');
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function save() {
    setErr(null);
    if (!name.trim()) {
      setErr('Name is required.');
      return;
    }
    setSaving(true);
    try {
      const res = await createKey({ name: name.trim() });
      notifySetupChanged();
      await onCreated(res.key);
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to create key');
    } finally {
      setSaving(false);
    }
  }

  if (alreadyRevealed) {
    return (
      <p className="text-sm text-emerald-300">
        Key generated — see the green banner above to copy it, then click &ldquo;I&apos;ve copied it — continue&rdquo;.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <div>
        <label className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">Key name</label>
        <input
          className="gatewayllm-input text-sm"
          placeholder="my-app"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      {err && <p className="text-sm text-red-400">{err}</p>}
      <div className="flex justify-end">
        <button type="button" className="gatewayllm-btn disabled:opacity-50" disabled={saving} onClick={save}>
          {saving ? 'Generating...' : 'Generate key'}
        </button>
      </div>
    </div>
  );
}

function TryItStep({
  deployments,
  rawKey,
  onComplete,
}: {
  deployments: DeploymentRow[];
  rawKey: string | null;
  onComplete: () => void;
}) {
  const [tab, setTab] = useState<'curl' | 'python'>('curl');
  const apiUrl = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const alias = deployments[0]?.model_alias ?? 'gpt-4o-mini';
  const token = rawKey ?? 'YOUR_GATEWAY_KEY';
  const curl = `curl ${apiUrl}/v1/chat/completions \\
  -H "Authorization: Bearer ${token}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${alias}",
    "messages": [{"role":"user","content":"Hello!"}]
  }'`;
  const python = `from openai import OpenAI

client = OpenAI(
    api_key="${token}",
    base_url="${apiUrl}/v1",
)

resp = client.chat.completions.create(
    model="${alias}",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(resp.choices[0].message.content)`;

  const snippet = tab === 'curl' ? curl : python;

  const tabCls = (t: 'curl' | 'python') =>
    `rounded-md px-3 py-1 text-xs font-medium transition-colors ${
      tab === t
        ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/40'
        : 'text-zinc-400 hover:text-zinc-200'
    }`;

  return (
    <div className="space-y-3">
      {rawKey ? (
        <p className="text-sm text-zinc-300">
          This snippet already has your new API key and route filled in. Paste it into your terminal
          or Python shell.
        </p>
      ) : (
        <p className="text-sm text-zinc-300">
          Send your first request. Replace{' '}
          <code className="text-emerald-300">YOUR_GATEWAY_KEY</code> with the key you just copied.
        </p>
      )}
      <div className="flex items-center justify-between gap-3">
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
      <pre className="overflow-x-auto rounded bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-200">
        {snippet}
      </pre>
      <p className="text-xs text-zinc-500">
        See usage, spend, and traces on the{' '}
        <Link href="/usage" className="text-emerald-400 hover:text-emerald-300">
          Usage
        </Link>{' '}
        page.
      </p>
      <div className="flex justify-end">
        <button type="button" className="gatewayllm-btn" onClick={onComplete}>
          I&apos;m all set
        </button>
      </div>
    </div>
  );
}
