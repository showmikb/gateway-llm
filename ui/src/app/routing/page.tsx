'use client';

import { useEffect, useMemo, useState } from 'react';

import { Alert, PageHeader } from '@/components/ui';
import {
  deleteRoutingPolicy,
  getRoutingPolicies,
  upsertRoutingPolicy,
} from '@/lib/api';
import type { RoutingPolicy } from '@/lib/types';

const STRATEGIES: Array<{
  value: string;
  label: string;
  hint: string;
}> = [
  {
    value: 'off',
    label: 'Off',
    hint: 'Disables smart routing for this alias entirely. Requests hit the configured deployment as-is and no ledger row is written.',
  },
  {
    value: 'track_only',
    label: 'Track only',
    hint: 'Smart router observes and writes a savings ledger row, but never overrides the alias. Safest default for new aliases.',
  },
  {
    value: 'auto_retry',
    label: 'Auto-retry on miss',
    hint: 'Routes to the cheaper alias. If the sampled judge scores the response below the quality threshold AND the request was non-streaming, the gateway retries against the baseline and serves the baseline response.',
  },
  {
    value: 'judge_then_decide',
    label: 'Judge then decide',
    hint: 'Runs the cheaper alias first, then runs an inline judge call. If the judge says "weak", swaps to the baseline before responding to the client. Adds latency but never serves a weak answer.',
  },
  {
    value: 'shadow_learn',
    label: 'Shadow learn',
    hint: 'Always serves the baseline. The cheaper alias is shadow-evaluated offline so the smart-route classifier keeps training without affecting customer traffic.',
  },
];

interface Draft extends Partial<RoutingPolicy> {
  model_alias: string;
  strategy: string;
  quality_threshold: number;
  retry_when_streaming: boolean;
  sample_pct: number;
  min_samples_before_routing: number;
}

const blankDraft = (): Draft => ({
  model_alias: '',
  strategy: 'track_only',
  quality_threshold: 0.7,
  retry_when_streaming: false,
  sample_pct: 5,
  min_samples_before_routing: 50,
});

export default function RoutingPage() {
  const [policies, setPolicies] = useState<RoutingPolicy[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft>(blankDraft());

  async function load() {
    setError(null);
    try {
      setLoading(true);
      const rows = await getRoutingPolicies();
      setPolicies(rows);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load policies');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  const strategyHint = useMemo(
    () => STRATEGIES.find((s) => s.value === draft.strategy)?.hint ?? '',
    [draft.strategy],
  );

  async function save() {
    if (!draft.model_alias.trim()) {
      setError('Alias is required');
      return;
    }
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await upsertRoutingPolicy(draft.model_alias.trim(), {
        strategy: draft.strategy,
        quality_threshold: draft.quality_threshold,
        baseline_provider_model: draft.baseline_provider_model || undefined,
        judge_alias: draft.judge_alias || undefined,
        retry_when_streaming: draft.retry_when_streaming,
        sample_pct: draft.sample_pct,
        min_samples_before_routing: draft.min_samples_before_routing,
      });
      setSuccess(`Saved policy for ${draft.model_alias}`);
      setDraft(blankDraft());
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }

  function edit(p: RoutingPolicy) {
    setDraft({
      ...p,
      model_alias: p.model_alias,
      strategy: p.strategy,
      quality_threshold: p.quality_threshold,
      retry_when_streaming: p.retry_when_streaming,
      sample_pct: p.sample_pct,
      min_samples_before_routing: p.min_samples_before_routing,
    });
    setSuccess(null);
    setError(null);
  }

  async function remove(alias: string) {
    if (!confirm(`Delete routing policy for ${alias}?`)) return;
    try {
      await deleteRoutingPolicy(alias);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Delete failed');
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Routing"
        description="Per-alias routing policy. Configure how aggressively the smart router replaces an alias with a cheaper alternative, and what quality safety net to use."
      />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}
      {success && <Alert variant="success" onDismiss={() => setSuccess(null)}>{success}</Alert>}

      <div className="gatewayllm-card space-y-4 p-4">
        <h2 className="text-sm font-semibold text-white">
          {draft.id ? `Edit policy for ${draft.model_alias}` : 'New / update policy'}
        </h2>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Model alias
            </span>
            <input
              className="gatewayllm-input w-full text-sm"
              placeholder="gpt-4-mini"
              value={draft.model_alias}
              onChange={(e) => setDraft({ ...draft, model_alias: e.target.value })}
              disabled={!!draft.id}
            />
          </label>

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Strategy
            </span>
            <select
              className="gatewayllm-input w-full text-sm"
              value={draft.strategy}
              onChange={(e) => setDraft({ ...draft, strategy: e.target.value })}
            >
              {STRATEGIES.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>

          {strategyHint && (
            <p className="text-xs text-zinc-500 sm:col-span-2">{strategyHint}</p>
          )}

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Quality threshold (0..1) — {draft.quality_threshold.toFixed(2)}
            </span>
            <input
              type="range"
              min={0}
              max={1}
              step={0.01}
              className="w-full"
              value={draft.quality_threshold}
              onChange={(e) =>
                setDraft({ ...draft, quality_threshold: parseFloat(e.target.value) })
              }
            />
          </label>

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Baseline provider/model (e.g. openai/gpt-4o)
            </span>
            <input
              className="gatewayllm-input w-full text-sm"
              placeholder="leave blank to use the alias's primary deployment"
              value={draft.baseline_provider_model ?? ''}
              onChange={(e) =>
                setDraft({ ...draft, baseline_provider_model: e.target.value })
              }
            />
          </label>

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Judge alias
            </span>
            <input
              className="gatewayllm-input w-full text-sm"
              placeholder="cheap-fast-judge"
              value={draft.judge_alias ?? ''}
              onChange={(e) => setDraft({ ...draft, judge_alias: e.target.value })}
            />
          </label>

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Sample % — {draft.sample_pct}
            </span>
            <input
              type="range"
              min={0}
              max={100}
              step={1}
              className="w-full"
              value={draft.sample_pct}
              onChange={(e) =>
                setDraft({ ...draft, sample_pct: parseInt(e.target.value, 10) })
              }
            />
          </label>

          <label className="space-y-1">
            <span className="text-xs font-medium uppercase tracking-wide text-zinc-500">
              Min samples before routing
            </span>
            <input
              type="number"
              min={0}
              className="gatewayllm-input w-full text-sm"
              value={draft.min_samples_before_routing}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  min_samples_before_routing: parseInt(e.target.value || '0', 10),
                })
              }
            />
          </label>

          <label className="flex items-center gap-2 text-sm text-zinc-300">
            <input
              type="checkbox"
              checked={draft.retry_when_streaming}
              onChange={(e) =>
                setDraft({ ...draft, retry_when_streaming: e.target.checked })
              }
            />
            Retry baseline even on streaming requests (best-effort)
          </label>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            className="gatewayllm-btn-primary py-1.5 text-sm"
            onClick={save}
            disabled={saving}
          >
            {saving ? 'Saving…' : 'Save policy'}
          </button>
          <button
            type="button"
            className="gatewayllm-btn-secondary py-1.5 text-sm"
            onClick={() => setDraft(blankDraft())}
          >
            Clear
          </button>
        </div>
      </div>

      <div className="gatewayllm-table-wrap">
        <div className="border-b border-zinc-800 bg-zinc-900/40 px-4 py-2 text-xs font-medium uppercase tracking-wide text-zinc-400">
          Existing policies
        </div>
        <table className="gatewayllm-table text-sm">
          <thead className="bg-zinc-900/40">
            <tr>
              <th className="gatewayllm-th">Alias</th>
              <th className="gatewayllm-th">Strategy</th>
              <th className="gatewayllm-th text-right">Threshold</th>
              <th className="gatewayllm-th">Baseline</th>
              <th className="gatewayllm-th">Judge</th>
              <th className="gatewayllm-th text-right">Sample %</th>
              <th className="gatewayllm-th text-right">Min samples</th>
              <th className="gatewayllm-th"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={8} className="gatewayllm-td text-center text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : policies.length === 0 ? (
              <tr>
                <td colSpan={8} className="gatewayllm-td text-center text-zinc-500">
                  No policies yet — every alias defaults to <code>track_only</code>.
                </td>
              </tr>
            ) : (
              policies.map((p) => (
                <tr key={p.id} className="hover:bg-zinc-900/40">
                  <td className="gatewayllm-td font-mono text-emerald-300/90">
                    {p.model_alias}
                    {!p.org_id && (
                      <span className="ml-2 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">
                        global
                      </span>
                    )}
                  </td>
                  <td className="gatewayllm-td">
                    <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-zinc-300">
                      {p.strategy}
                    </span>
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {p.quality_threshold.toFixed(2)}
                  </td>
                  <td className="gatewayllm-td font-mono text-xs text-zinc-300">
                    {p.baseline_provider_model || '—'}
                  </td>
                  <td className="gatewayllm-td font-mono text-xs text-zinc-300">
                    {p.judge_alias || '—'}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">{p.sample_pct}</td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {p.min_samples_before_routing}
                  </td>
                  <td className="gatewayllm-td">
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        className="text-xs text-emerald-400 hover:text-emerald-300"
                        onClick={() => edit(p)}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        className="text-xs text-red-400 hover:text-red-300"
                        onClick={() => remove(p.model_alias)}
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
