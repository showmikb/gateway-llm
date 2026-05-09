'use client';

import Link from 'next/link';
import { useEffect, useState } from 'react';

import { Alert, PageHeader } from '@/components/ui';
import { verifySavings } from '@/lib/api';
import type { RoutingSavings, SavingsVerifyResult } from '@/lib/types';

function fmtUSD(n: number, max = 6): string {
  if (!isFinite(n)) return '$0.00';
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: max })}`;
}

function Field({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div>
      <div className="text-[11px] font-medium uppercase tracking-wide text-zinc-500">{label}</div>
      <div className={`mt-0.5 text-sm text-zinc-200 ${mono ? 'font-mono' : ''}`}>{value}</div>
    </div>
  );
}

export interface SavingsDrillDownProps {
  loader: () => Promise<RoutingSavings | null>;
  loaderKey: string;
  backHref: string;
  backLabel: string;
}

export function SavingsDrillDown({ loader, loaderKey, backHref, backLabel }: SavingsDrillDownProps) {
  const [row, setRow] = useState<RoutingSavings | null>(null);
  const [verify, setVerify] = useState<SavingsVerifyResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        setError(null);
        const r = await loader();
        if (!cancelled) setRow(r);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // loaderKey covers both /savings/[id] and /usage/[trace]
  }, [loaderKey]); // eslint-disable-line react-hooks/exhaustive-deps

  async function runVerify() {
    if (!row) return;
    try {
      setVerifying(true);
      setError(null);
      const v = await verifySavings(row.id);
      setVerify(v);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Verification failed');
    } finally {
      setVerifying(false);
    }
  }

  if (!row) {
    return (
      <div className="space-y-6">
        <PageHeader
          title="Savings record"
          description={loading ? 'Loading…' : 'Not found'}
          action={
            <Link href={backHref} className="gatewayllm-btn-secondary py-1.5 text-sm">
              ← {backLabel}
            </Link>
          }
        />
        {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}
        {!loading && !error && (
          <div className="gatewayllm-card p-6 text-sm text-zinc-400">
            We couldn&apos;t find a signed savings record for this request. Either it predates the
            smart-routing ledger, or it didn&apos;t flow through a routing policy.
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={`Saved ${fmtUSD(row.savings_usd, 6)}`}
        description={`Trace ${row.trace_id} · routed at ${new Date(row.created_at).toLocaleString()}`}
        action={
          <div className="flex items-center gap-2">
            <Link href={backHref} className="gatewayllm-btn-secondary py-1.5 text-sm">
              ← {backLabel}
            </Link>
            <button
              type="button"
              className="gatewayllm-btn-primary py-1.5 text-sm"
              disabled={verifying}
              onClick={runVerify}
            >
              {verifying ? 'Verifying…' : 'Verify signature'}
            </button>
          </div>
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      {verify && (
        <Alert variant={verify.verified ? 'success' : 'error'} onDismiss={() => setVerify(null)}>
          {verify.verified ? (
            <span>
              <strong>Verified.</strong> Signed by key{' '}
              <code className="font-mono">{verify.signed_by_key_id}</code>
              {verify.signed_at && <> at {new Date(verify.signed_at).toLocaleString()}</>}.
            </span>
          ) : (
            <span>
              <strong>Verification failed:</strong> {verify.reason ?? 'unknown'}
            </span>
          )}
        </Alert>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="gatewayllm-card lg:col-span-2 space-y-4 p-4">
          <h2 className="text-sm font-semibold text-white">Routing decision</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <Field label="Requested alias" value={row.requested_alias} mono />
            <Field label="Served alias" value={row.served_alias} mono />
            <Field label="Strategy" value={row.strategy || '—'} />
            <Field
              label="Complexity"
              value={`${row.complexity_score.toFixed(3)} (${row.complexity_bucket || '—'})`}
            />
            <Field label="Overridden" value={row.overridden ? 'yes' : 'no'} />
            <Field label="Retried" value={row.retried ? 'yes' : 'no'} />
            <Field
              label="Served provider/model"
              value={`${row.served_provider ?? '—'} / ${row.served_model ?? '—'}`}
              mono
            />
            <Field
              label="Baseline provider/model"
              value={`${row.baseline_provider ?? '—'} / ${row.baseline_model ?? '—'}`}
              mono
            />
            <Field
              label="Discount applied"
              value={`${(row.discount_pct_applied * 100).toFixed(2)}%`}
            />
          </div>

          <div className="border-t border-zinc-800 pt-4">
            <h2 className="mb-3 text-sm font-semibold text-white">Cost breakdown</h2>
            <table className="gatewayllm-table text-sm">
              <thead>
                <tr>
                  <th className="gatewayllm-th"></th>
                  <th className="gatewayllm-th text-right">Input tokens</th>
                  <th className="gatewayllm-th text-right">Output tokens</th>
                  <th className="gatewayllm-th text-right">Cost USD</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-800">
                <tr>
                  <td className="gatewayllm-td font-mono text-zinc-400">baseline</td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {row.baseline_input_tokens}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {row.baseline_output_tokens}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {fmtUSD(row.baseline_cost_usd, 6)}
                  </td>
                </tr>
                <tr>
                  <td className="gatewayllm-td font-mono text-zinc-200">served</td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {row.served_input_tokens}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {row.served_output_tokens}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums">
                    {fmtUSD(row.actual_cost_usd, 6)}
                  </td>
                </tr>
                <tr className="bg-emerald-900/10">
                  <td className="gatewayllm-td font-mono text-emerald-300">saved</td>
                  <td className="gatewayllm-td"></td>
                  <td className="gatewayllm-td"></td>
                  <td className="gatewayllm-td text-right tabular-nums text-emerald-300">
                    {fmtUSD(row.savings_usd, 6)}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div className="space-y-4">
          <div className="gatewayllm-card space-y-3 p-4">
            <h2 className="text-sm font-semibold text-white">Quality</h2>
            <Field
              label="Score"
              value={row.quality_score != null ? row.quality_score.toFixed(3) : 'not yet scored'}
            />
            <Field
              label="Pass"
              value={row.quality_pass == null ? '—' : row.quality_pass ? 'pass' : 'fail'}
            />
            <Field label="Scorer" value={row.quality_scorer || '—'} mono />
          </div>

          <div className="gatewayllm-card space-y-3 p-4">
            <h2 className="text-sm font-semibold text-white">Signature</h2>
            <Field label="Signed by key" value={row.signed_by_key_id || '—'} mono />
            <Field
              label="Signed at"
              value={row.signed_at ? new Date(row.signed_at).toLocaleString() : '—'}
            />
            <Field
              label="Prev hash"
              value={row.prev_hash ? `${row.prev_hash.slice(0, 16)}…` : '—'}
              mono
            />
            <Field
              label="Signature"
              value={row.signature ? `${row.signature.slice(0, 24)}…` : '—'}
              mono
            />
          </div>

          {row.recording_id && (
            <div className="gatewayllm-card space-y-2 p-4">
              <h2 className="text-sm font-semibold text-white">Replay</h2>
              <Field label="Recording" value={row.recording_id} mono />
              <p className="text-xs text-zinc-500">
                The full prompt + response is stored in the recordings blob; head to the
                Observability page to replay it.
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
