'use client';

import { useEffect, useMemo, useState } from 'react';
import {
  getCredentials,
  createCredential,
  deleteCredential,
  testCredential,
  testRawCredential,
  getOrganizations,
  listProviders,
} from '@/lib/api';
import type { ProviderCredential, Organization, ProviderInfo } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, EmptyState, Badge, InfoTooltip } from '@/components/ui';
import { FirstVisitTip } from '@/components/FirstVisitTip';
import { notifySetupChanged } from '@/lib/setupState';

interface ProviderOption {
  id: string;
  label: string;
  apiBaseRequired: boolean;
  apiBaseHint?: string;
  apiKeyHint?: string;
}

// Static fallback used only when the /v1/management/providers endpoint
// fails. Production runs should always go through the live endpoint so
// new providers added to the gateway show up here without a UI deploy.
const FALLBACK_PROVIDERS: ProviderOption[] = [
  { id: 'openai', label: 'OpenAI', apiBaseRequired: false, apiKeyHint: 'sk-...' },
  { id: 'anthropic', label: 'Anthropic', apiBaseRequired: false, apiKeyHint: 'sk-ant-...' },
  { id: 'gemini', label: 'Google Gemini', apiBaseRequired: false, apiKeyHint: 'AIza...' },
  {
    id: 'azure',
    label: 'Azure OpenAI',
    apiBaseRequired: true,
    apiBaseHint: 'https://<your-resource>.openai.azure.com',
    apiKeyHint: 'Azure resource key',
  },
  {
    id: 'bedrock',
    label: 'AWS Bedrock',
    apiBaseRequired: true,
    apiBaseHint: 'https://bedrock-runtime.<region>.amazonaws.com',
    apiKeyHint: 'Bedrock API key (Bearer token)',
  },
  { id: 'cohere', label: 'Cohere', apiBaseRequired: false },
  { id: 'groq', label: 'Groq', apiBaseRequired: false, apiKeyHint: 'gsk_...' },
  { id: 'mistral', label: 'Mistral', apiBaseRequired: false },
  {
    id: 'vertexai',
    label: 'Google Vertex AI',
    apiBaseRequired: true,
    apiBaseHint: 'https://<region>-aiplatform.googleapis.com',
    apiKeyHint: 'Service-account access token',
  },
  { id: 'xai', label: 'xAI (Grok)', apiBaseRequired: false, apiKeyHint: 'xai-...' },
];

function toProviderOption(info: ProviderInfo): ProviderOption {
  return {
    id: info.id,
    label: info.label,
    apiBaseRequired: info.requires_api_base,
    apiBaseHint: info.api_base_hint,
    apiKeyHint: info.api_key_hint,
  };
}

export default function CredentialsPage() {
  const [creds, setCreds] = useState<ProviderCredential[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [providersList, setProvidersList] = useState<ProviderOption[]>(FALLBACK_PROVIDERS);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [testPopup, setTestPopup] = useState<
    | { state: 'loading' }
    | { state: 'ok'; message: string }
    | { state: 'error'; message: string }
    | null
  >(null);

  const [name, setName] = useState('');
  const [provider, setProvider] = useState<string>('openai');
  const [apiKey, setApiKey] = useState('');
  const [apiBase, setApiBase] = useState('');
  const [orgId, setOrgId] = useState('');
  const [testing, setTesting] = useState(false);
  const [createTestResult, setCreateTestResult] = useState<{ status: string; message: string } | null>(null);

  const selectedProvider = useMemo(
    () => providersList.find((p) => p.id === provider) ?? providersList[0],
    [providersList, provider],
  );

  const orgMap = Object.fromEntries(orgs.map((o) => [o.id, o.name]));

  async function load() {
    try {
      const [data, orgData, providerData] = await Promise.all([
        getCredentials(),
        getOrganizations(),
        listProviders().catch(() => [] as ProviderInfo[]),
      ]);
      setCreds(data);
      setOrgs(orgData);
      if (providerData.length > 0) {
        setProvidersList(providerData.map(toProviderOption));
      }
      setError('');
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function handleCreate() {
    try {
      await createCredential({
        name,
        provider,
        api_key: apiKey,
        api_base: apiBase || undefined,
        org_id: orgId || undefined,
      });
      setName('');
      setApiKey('');
      setApiBase('');
      setOrgId('');
      setCreateTestResult(null);
      setShowCreate(false);
      notifySetupChanged();
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Create failed');
    }
  }

  async function handleTestRaw() {
    setCreateTestResult(null);
    setTesting(true);
    try {
      const result = await testRawCredential({
        provider,
        api_key: apiKey,
        api_base: apiBase || undefined,
      });
      setCreateTestResult(result);
    } catch (e: unknown) {
      setCreateTestResult({
        status: 'error',
        message: e instanceof Error ? e.message : 'Test failed',
      });
    } finally {
      setTesting(false);
    }
  }

  async function handleDelete(id: string) {
    if (!confirm('Delete this credential? Any model routes still pointing at it must be removed first.')) return;
    try {
      await deleteCredential(id);
      notifySetupChanged();
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  async function handleTest(id: string) {
    setTestPopup({ state: 'loading' });
    try {
      const result = await testCredential(id);
      if (result.status === 'ok') {
        setTestPopup({ state: 'ok', message: result.message || 'Connection verified.' });
      } else {
        setTestPopup({ state: 'error', message: result.message || 'Test failed' });
      }
    } catch (e: unknown) {
      setTestPopup({
        state: 'error',
        message: e instanceof Error ? e.message : 'Test failed',
      });
    }
  }

  useEffect(() => {
    if (!testPopup || testPopup.state === 'loading') return;
    const ms = testPopup.state === 'ok' ? 2000 : 3500;
    const t = setTimeout(() => setTestPopup(null), ms);
    return () => clearTimeout(t);
  }, [testPopup]);

  if (loading) return <LoadingSkeleton rows={4} />;

  return (
    <div className="space-y-6">
      <FirstVisitTip tipId="credentials" title="Start by adding a provider key">
        Paste an API key from any supported provider — OpenAI, Anthropic, Gemini, Azure OpenAI, AWS
        Bedrock, and more. Gateway-LLM encrypts it at rest and uses it to call the underlying model
        on your behalf.
      </FirstVisitTip>
      <PageHeader
        title="Provider Keys"
        description="Store and manage API keys for OpenAI, Anthropic, Gemini, Azure OpenAI, AWS Bedrock, and other providers."
        action={
          <button onClick={() => setShowCreate(!showCreate)} className="gatewayllm-btn">
            {showCreate ? 'Cancel' : 'Add Provider Key'}
          </button>
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError('')}>{error}</Alert>}

      {showCreate && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">Add Provider Key</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Name<InfoTooltip text="A label to identify this key (e.g. 'acme-openai-prod')." /></label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="gatewayllm-input"
                placeholder="openai-prod"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Provider</label>
              <select
                value={provider}
                onChange={(e) => {
                  setProvider(e.target.value);
                  setCreateTestResult(null);
                }}
                className="gatewayllm-input"
              >
                {providersList.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">API Key<InfoTooltip text="Your provider API key. Stored encrypted; never shown again in full." /></label>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value);
                  setCreateTestResult(null);
                }}
                className="gatewayllm-input"
                placeholder={selectedProvider.apiKeyHint || 'sk-...'}
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">
                API Base{selectedProvider.apiBaseRequired ? '' : ' (optional)'}
              </label>
              <input
                value={apiBase}
                onChange={(e) => {
                  setApiBase(e.target.value);
                  setCreateTestResult(null);
                }}
                className="gatewayllm-input"
                placeholder={selectedProvider.apiBaseHint || 'https://api.openai.com'}
              />
              {selectedProvider.apiBaseRequired && (
                <p className="mt-1 text-[11px] text-amber-300/80">
                  Required for {selectedProvider.label}.
                </p>
              )}
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Organization<InfoTooltip text="Assign this key to an org for cost isolation." /></label>
              <select
                value={orgId}
                onChange={(e) => setOrgId(e.target.value)}
                className="gatewayllm-input"
              >
                <option value="">Global (all orgs)</option>
                {orgs.filter((o) => o.is_active).map((o) => (
                  <option key={o.id} value={o.id}>{o.name}</option>
                ))}
              </select>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <button
              type="button"
              onClick={handleTestRaw}
              disabled={
                testing ||
                !apiKey ||
                (selectedProvider.apiBaseRequired && !apiBase.trim())
              }
              className="gatewayllm-btn-secondary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {testing ? 'Testing…' : 'Test Connection'}
            </button>
            <button
              onClick={handleCreate}
              disabled={
                !name ||
                !apiKey ||
                (selectedProvider.apiBaseRequired && !apiBase.trim())
              }
              className="gatewayllm-btn disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Save Provider Key
            </button>
            {createTestResult && (
              <span
                className={`text-xs ${
                  createTestResult.status === 'ok' ? 'text-emerald-400' : 'text-red-400'
                }`}
              >
                {createTestResult.status === 'ok'
                  ? `Connected — ${createTestResult.message}`
                  : createTestResult.message}
              </span>
            )}
          </div>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead>
            <tr>
              <th className="gatewayllm-th">Name</th>
              <th className="gatewayllm-th">Provider</th>
              <th className="gatewayllm-th">Key</th>
              <th className="gatewayllm-th">Organization</th>
              <th className="gatewayllm-th">API Base</th>
              <th className="gatewayllm-th">Status</th>
              <th className="gatewayllm-th text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {creds.length === 0 ? (
              <tr>
                <td colSpan={7}>
                  <EmptyState
                    message="No provider keys configured."
                    action={() => setShowCreate(true)}
                    actionLabel="Add your first provider key"
                  />
                </td>
              </tr>
            ) : (
              creds.map((c) => (
                <tr key={c.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                  <td className="px-4 py-3 font-medium text-white">{c.name}</td>
                  <td className="px-4 py-3 text-zinc-300">
                    {providersList.find((p) => p.id === c.provider)?.label ?? c.provider}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-zinc-400">{c.api_key_masked}</td>
                  <td className="px-4 py-3 text-sm text-zinc-400">{c.org_id ? orgMap[c.org_id] || c.org_id : 'Global'}</td>
                  <td className="px-4 py-3 font-mono text-xs text-zinc-500">{c.api_base || '—'}</td>
                  <td className="px-4 py-3">
                    <Badge variant={c.is_active ? 'success' : 'danger'}>
                      {c.is_active ? 'Active' : 'Inactive'}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-right space-x-2">
                    <button
                      onClick={() => handleTest(c.id)}
                      className="text-xs text-emerald-400 hover:text-emerald-300"
                    >
                      Test
                    </button>
                    <button
                      onClick={() => handleDelete(c.id)}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      {testPopup && (
        <CredentialTestPopup popup={testPopup} onClose={() => setTestPopup(null)} />
      )}
    </div>
  );
}

function CredentialTestPopup({
  popup,
  onClose,
}: {
  popup:
    | { state: 'loading' }
    | { state: 'ok'; message: string }
    | { state: 'error'; message: string };
  onClose: () => void;
}) {
  const isLoading = popup.state === 'loading';
  const isOk = popup.state === 'ok';
  const isError = popup.state === 'error';
  const accentRing = isOk
    ? 'ring-emerald-500/40'
    : isError
    ? 'ring-red-500/40'
    : 'ring-zinc-700';

  return (
    <div
      className="gatewayllm-popup-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={isLoading ? undefined : onClose}
      role="dialog"
      aria-modal="true"
      aria-live="polite"
    >
      <div
        className={`gatewayllm-popup-card relative flex min-w-[260px] flex-col items-center gap-3 rounded-2xl border border-zinc-800 bg-zinc-900/95 px-8 py-7 shadow-2xl ring-1 ${accentRing}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="relative h-20 w-20">
          {isLoading ? (
            <>
              <div className="gatewayllm-popup-pulse absolute inset-0 rounded-full bg-emerald-500/15" />
              <svg
                className="gatewayllm-popup-spinner absolute inset-0"
                viewBox="0 0 50 50"
                fill="none"
                aria-hidden
              >
                <circle cx="25" cy="25" r="22" stroke="#27272a" strokeWidth="3" />
                <path
                  d="M25 3 a22 22 0 0 1 22 22"
                  stroke="#10b981"
                  strokeWidth="3"
                  strokeLinecap="round"
                />
              </svg>
            </>
          ) : (
            <svg viewBox="0 0 60 60" fill="none" aria-hidden>
              <circle
                className="gatewayllm-popup-circle"
                cx="30"
                cy="30"
                r="26.5"
                stroke={isOk ? '#10b981' : '#ef4444'}
                strokeWidth="3"
                strokeLinecap="round"
                fill="none"
              />
              {isOk ? (
                <path
                  className="gatewayllm-popup-check"
                  d="M18 31 l8 8 l16 -16"
                  stroke="#10b981"
                  strokeWidth="3.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  fill="none"
                />
              ) : (
                <>
                  <path
                    className="gatewayllm-popup-cross"
                    d="M20 20 L40 40"
                    stroke="#ef4444"
                    strokeWidth="3.5"
                    strokeLinecap="round"
                    fill="none"
                  />
                  <path
                    className="gatewayllm-popup-cross"
                    d="M40 20 L20 40"
                    stroke="#ef4444"
                    strokeWidth="3.5"
                    strokeLinecap="round"
                    fill="none"
                  />
                </>
              )}
            </svg>
          )}
        </div>
        <p
          className={`text-base font-semibold ${
            isOk ? 'text-emerald-300' : isError ? 'text-red-300' : 'text-zinc-200'
          }`}
        >
          {isLoading ? 'Testing connection…' : isOk ? 'Valid' : 'Invalid'}
        </p>
        {!isLoading && (
          <p className="max-w-[280px] text-center text-xs text-zinc-400">
            {popup.state === 'ok' || popup.state === 'error' ? popup.message : ''}
          </p>
        )}
      </div>
    </div>
  );
}
