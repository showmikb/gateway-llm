'use client';

import { useEffect, useState } from 'react';
import { getConfig, updateConfig } from '@/lib/api';
import type { GatewayConfig } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, CopyButton } from '@/components/ui';

export default function SettingsPage() {
  const [config, setConfig] = useState<GatewayConfig | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');

  const [strategy, setStrategy] = useState('round-robin');
  const [retries, setRetries] = useState('2');
  const [retryDelayMs, setRetryDelayMs] = useState('500');
  const [fallbackEnabled, setFallbackEnabled] = useState(true);
  const [defaultRpm, setDefaultRpm] = useState('60');
  const [defaultTpm, setDefaultTpm] = useState('100000');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setError(null);
        const cfg = await getConfig();
        if (!cancelled) {
          setConfig(cfg);
          const r = cfg.routing as Record<string, unknown> | undefined;
          const rl = cfg.rate_limiting as Record<string, unknown> | undefined;
          if (r) {
            setStrategy(String(r.strategy ?? 'round-robin'));
            setRetries(String(r.retries ?? 2));
            setRetryDelayMs(String(r.retry_delay_ms ?? 500));
            setFallbackEnabled(Boolean(r.fallback_enabled ?? true));
          }
          if (rl) {
            setDefaultRpm(String(rl.default_rpm ?? 60));
            setDefaultTpm(String(rl.default_tpm ?? 100000));
          }
        }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load config');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  async function handleSave() {
    setSaving(true);
    setMsg('');
    setError(null);
    try {
      await updateConfig({
        routing: {
          strategy,
          retries: parseInt(retries),
          retry_delay_ms: parseInt(retryDelayMs),
          fallback_enabled: fallbackEnabled,
        },
        rate_limiting: {
          default_rpm: parseInt(defaultRpm),
          default_tpm: parseInt(defaultTpm),
        },
      });
      setMsg('Settings saved.');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  if (loading) return <LoadingSkeleton rows={4} />;

  return (
    <div className="space-y-6">
      <PageHeader title="Settings" description="Configure routing, rate limiting, and logging." />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}
      {msg && <Alert variant="success" onDismiss={() => setMsg('')}>{msg}</Alert>}

      <div className="grid gap-6 md:grid-cols-2">
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">Routing</h2>
          <div>
            <label className="block text-sm text-zinc-400 mb-1">Strategy</label>
            <select
              value={strategy}
              onChange={(e) => setStrategy(e.target.value)}
              className="gatewayllm-input"
            >
              <option value="round-robin">Round Robin</option>
              <option value="least-latency">Least Latency</option>
            </select>
          </div>
          <div>
            <label className="block text-sm text-zinc-400 mb-1">Retries</label>
            <input
              type="number"
              min="0"
              value={retries}
              onChange={(e) => setRetries(e.target.value)}
              className="gatewayllm-input"
            />
          </div>
          <div>
            <label className="block text-sm text-zinc-400 mb-1">Retry Delay (ms)</label>
            <input
              type="number"
              min="0"
              value={retryDelayMs}
              onChange={(e) => setRetryDelayMs(e.target.value)}
              className="gatewayllm-input"
            />
          </div>
          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              checked={fallbackEnabled}
              onChange={(e) => setFallbackEnabled(e.target.checked)}
              className="rounded border-zinc-600 bg-zinc-800 text-emerald-500 focus:ring-emerald-500/20"
            />
            Enable fallback
          </label>
        </div>

        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">Rate Limiting</h2>
          <div>
            <label className="block text-sm text-zinc-400 mb-1">Default RPM</label>
            <input
              type="number"
              min="0"
              value={defaultRpm}
              onChange={(e) => setDefaultRpm(e.target.value)}
              className="gatewayllm-input"
            />
          </div>
          <div>
            <label className="block text-sm text-zinc-400 mb-1">Default TPM</label>
            <input
              type="number"
              min="0"
              value={defaultTpm}
              onChange={(e) => setDefaultTpm(e.target.value)}
              className="gatewayllm-input"
            />
          </div>
        </div>
      </div>

      <div className="flex gap-3">
        <button
          onClick={handleSave}
          disabled={saving}
          className="gatewayllm-btn px-6 py-2.5 disabled:opacity-50"
        >
          {saving ? 'Saving...' : 'Save Settings'}
        </button>
      </div>

      {config && (
        <details className="gatewayllm-card overflow-hidden">
          <summary className="cursor-pointer px-5 py-3 text-sm font-medium text-zinc-400 hover:text-zinc-200">
            Raw Configuration (read-only)
          </summary>
          <div className="border-t border-zinc-800 p-3">
            <div className="mb-2 flex justify-end">
              <CopyButton value={JSON.stringify(config, null, 2)} label="Copy JSON" />
            </div>
            <pre className="max-h-[50vh] overflow-auto p-2 font-mono text-xs leading-relaxed text-zinc-300">
              {JSON.stringify(config, null, 2)}
            </pre>
          </div>
        </details>
      )}
    </div>
  );
}
