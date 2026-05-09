'use client';

import { useEffect, useState } from 'react';
import { getCallbacks, testCallbacks, getConfig } from '@/lib/api';
import type { CallbackInfo, GatewayConfig } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, Badge, CopyButton } from '@/components/ui';
import { useAuth } from '@/contexts/AuthContext';

export default function ObservabilityPage() {
  const { isAdmin } = useAuth();
  const [cbs, setCbs] = useState<CallbackInfo[]>([]);
  const [configCallbacks, setConfigCallbacks] = useState<GatewayConfig['callbacks']>([]);
  const [testResults, setTestResults] = useState<Record<string, string> | null>(null);
  const [loading, setLoading] = useState(true);
  const [testing, setTesting] = useState(false);
  const [error, setError] = useState('');

  async function load() {
    try {
      const [cbList, config] = await Promise.all([getCallbacks(), getConfig()]);
      setCbs(cbList);
      setConfigCallbacks(config.callbacks ?? []);
      setError('');
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load callbacks');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, []);

  async function handleTest() {
    setTesting(true);
    setTestResults(null);
    try {
      const results = await testCallbacks();
      setTestResults(results);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Test failed');
    } finally {
      setTesting(false);
    }
  }

  if (loading) return <LoadingSkeleton rows={3} />;

  const hasCallbacks = cbs.length > 0 || (configCallbacks && configCallbacks.length > 0);

  return (
    <div className="space-y-6">
      <PageHeader
        title="Observability"
        description="View and test configured log forwarding destinations."
        action={
          hasCallbacks && isAdmin ? (
            <button onClick={handleTest} disabled={testing} className="gatewayllm-btn disabled:opacity-50">
              {testing ? 'Testing...' : 'Test All Callbacks'}
            </button>
          ) : undefined
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError('')}>{error}</Alert>}

      {testResults && (
        <div className="gatewayllm-card p-5 space-y-3">
          <h2 className="text-lg font-medium text-white">Test Results</h2>
          <div className="space-y-2">
            {Object.entries(testResults).map(([name, result]) => (
              <div key={name} className="flex items-center justify-between rounded-lg bg-zinc-800/50 px-4 py-2">
                <span className="text-sm text-white">{name}</span>
                <Badge variant={result === 'ok' ? 'success' : 'danger'}>
                  {result}
                </Badge>
              </div>
            ))}
          </div>
        </div>
      )}

      {!hasCallbacks ? (
        <div className="gatewayllm-card p-8 text-center">
          <p className="text-zinc-400">No callbacks configured.</p>
          <p className="mt-2 text-sm text-zinc-500">
            Add callback destinations in your <code className="text-zinc-300">config.yaml</code> under the{' '}
            <code className="text-zinc-300">callbacks</code> section.
          </p>
          <div className="mt-4 text-left">
            <div className="mb-2 flex justify-end">
              <CopyButton
                value={`callbacks:
  - type: otel
    endpoint: "http://otel-collector:4318"
    service_name: gateway-llm

  - type: datadog
    api_key_env: DD_API_KEY

  - type: langsmith
    api_key_env: LANGSMITH_API_KEY
    project: "my-project"

  - type: webhook
    endpoint: "https://example.com/logs"
    method: POST`}
                label="Copy YAML"
              />
            </div>
            <pre className="rounded-lg bg-zinc-800 p-4 text-xs text-zinc-300 overflow-x-auto">
{`callbacks:
  - type: otel
    endpoint: "http://otel-collector:4318"
    service_name: gateway-llm

  - type: datadog
    api_key_env: DD_API_KEY

  - type: langsmith
    api_key_env: LANGSMITH_API_KEY
    project: "my-project"

  - type: webhook
    endpoint: "https://example.com/logs"
    method: POST`}
            </pre>
          </div>
        </div>
      ) : (
        <div className="gatewayllm-table-wrap">
          <table className="gatewayllm-table">
            <thead>
              <tr>
                <th className="gatewayllm-th">Type</th>
                <th className="gatewayllm-th">Endpoint / Target</th>
                <th className="gatewayllm-th">Details</th>
              </tr>
            </thead>
            <tbody>
              {(configCallbacks && configCallbacks.length > 0 ? configCallbacks : cbs).map((cb, idx) => {
                const type_ = 'type' in cb ? cb.type : '';
                const endpoint = 'endpoint' in cb ? (cb.endpoint ?? '') : '';
                const detail =
                  'service_name' in cb && cb.service_name
                    ? `service: ${cb.service_name}`
                    : 'project' in cb && cb.project
                      ? `project: ${cb.project}`
                      : 'model_id' in cb && cb.model_id
                        ? `model: ${cb.model_id}`
                        : '—';
                return (
                  <tr key={idx} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                    <td className="px-4 py-3">
                      <span className="inline-block rounded-full bg-emerald-900/40 px-2.5 py-0.5 text-xs font-medium text-emerald-300 ring-1 ring-emerald-700/50">
                        {type_}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-white font-mono text-xs">
                      {endpoint || '—'}
                    </td>
                    <td className="px-4 py-3 text-zinc-400">{detail}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <div className="gatewayllm-card p-5">
        <h2 className="text-lg font-medium text-white mb-3">Supported Destinations</h2>
        <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
          {[
            { name: 'OpenTelemetry', desc: 'OTLP HTTP/gRPC traces', type: 'otel' },
            { name: 'Datadog', desc: 'Logs API v2', type: 'datadog' },
            { name: 'LangSmith', desc: 'LangChain tracing', type: 'langsmith' },
            { name: 'Arize', desc: 'ML observability', type: 'arize' },
            { name: 'Webhook', desc: 'Generic HTTP POST', type: 'webhook' },
            { name: 'MLflow', desc: 'Via webhook type', type: 'webhook' },
          ].map((dest) => (
            <div key={dest.name} className="rounded-lg bg-zinc-800/50 p-3">
              <p className="text-sm font-medium text-white">{dest.name}</p>
              <p className="text-xs text-zinc-400">{dest.desc}</p>
              <code className="mt-1 inline-block text-xs text-zinc-500">type: {dest.type}</code>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
