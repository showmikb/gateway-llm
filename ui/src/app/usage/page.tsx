'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { getUsage } from '@/lib/api';
import type { SpendLog } from '@/lib/types';
import { PageHeader, Alert } from '@/components/ui';

const PAGE_SIZE = 25;

export default function UsagePage() {
  const router = useRouter();
  const [logs, setLogs] = useState<SpendLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(0);

  const load = useCallback(async () => {
    setError(null);
    const offset = page * PAGE_SIZE;
    const data = await getUsage(PAGE_SIZE, offset);
    setLogs(data);
  }, [page]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        await load();
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load usage');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [load]);

  const canPrev = page > 0;
  const canNext = logs.length === PAGE_SIZE;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Usage"
        description="Recent spend logs from the gateway."
        action={
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-sm"
              disabled={!canPrev || loading}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
            >
              Previous
            </button>
            <span className="text-sm text-zinc-500">
              Page <span className="tabular-nums text-zinc-300">{page + 1}</span>
            </span>
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-sm"
              disabled={!canNext || loading}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      <p className="text-xs text-zinc-500">
        Click a row to open its routing decision drill-down (baseline vs. actual cost, quality
        score, signature). Older requests without a trace ID aren&apos;t clickable.
      </p>

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table text-xs sm:text-sm">
          <thead className="bg-zinc-900/80">
            <tr>
              <th className="gatewayllm-th">Model</th>
              <th className="gatewayllm-th">Provider</th>
              <th className="gatewayllm-th">Endpoint</th>
              <th className="gatewayllm-th text-right">Tokens</th>
              <th className="gatewayllm-th text-right">Cost</th>
              <th className="gatewayllm-th text-right">Latency</th>
              <th className="gatewayllm-th">Time</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={7} className="gatewayllm-td text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : logs.length === 0 ? (
              <tr>
                <td colSpan={7} className="gatewayllm-td text-zinc-500">
                  No usage logs yet.
                </td>
              </tr>
            ) : (
              logs.map((u) => {
                const drilldown = u.trace_id
                  ? `/usage/${encodeURIComponent(u.trace_id)}`
                  : null;
                return (
                  <tr
                    key={u.id}
                    className={
                      drilldown
                        ? 'cursor-pointer hover:bg-zinc-900/60'
                        : 'hover:bg-zinc-900/40'
                    }
                    onClick={() => {
                      if (drilldown) router.push(drilldown);
                    }}
                    onKeyDown={(e) => {
                      if (drilldown && (e.key === 'Enter' || e.key === ' ')) {
                        e.preventDefault();
                        router.push(drilldown);
                      }
                    }}
                    role={drilldown ? 'link' : undefined}
                    tabIndex={drilldown ? 0 : -1}
                    title={drilldown ? 'View routing decision' : undefined}
                  >
                    <td className="gatewayllm-td font-mono text-emerald-300/90">
                      {u.model_alias}
                    </td>
                    <td className="gatewayllm-td capitalize text-zinc-300">{u.provider}</td>
                    <td className="gatewayllm-td text-zinc-400">{u.endpoint}</td>
                    <td className="gatewayllm-td text-right tabular-nums text-zinc-300">
                      {u.total_tokens}
                    </td>
                    <td className="gatewayllm-td text-right tabular-nums text-zinc-300">
                      ${u.cost_usd.toFixed(6)}
                    </td>
                    <td className="gatewayllm-td text-right tabular-nums text-zinc-400">
                      {u.latency_ms} ms
                    </td>
                    <td className="gatewayllm-td whitespace-nowrap text-zinc-500">
                      {new Date(u.created_at).toLocaleString()}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
