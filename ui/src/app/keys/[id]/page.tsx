'use client';

import Link from 'next/link';
import { useParams } from 'next/navigation';
import { useEffect, useMemo, useState } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { getKeyDailyUsage, getKeyUsage } from '@/lib/api';
import type { APIKey, DailySpend, SpendLog } from '@/lib/types';
import { Alert, Badge, LoadingSkeleton, PageHeader, StatCard } from '@/components/ui';

const PAGE_SIZE = 50;

function fmtUSD(n: number, max = 4): string {
  if (!isFinite(n)) return '$0.00';
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: max })}`;
}

function fmtNum(n: number): string {
  return n.toLocaleString();
}

export default function KeyDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? '';

  const [key, setKey] = useState<APIKey | null>(null);
  const [logs, setLogs] = useState<SpendLog[]>([]);
  const [daily, setDaily] = useState<DailySpend[]>([]);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    (async () => {
      try {
        setLoading(true);
        setError(null);
        const [usage, dailyData] = await Promise.all([
          getKeyUsage(id, PAGE_SIZE, page * PAGE_SIZE),
          page === 0 ? getKeyDailyUsage(id, 30) : Promise.resolve(daily),
        ]);
        if (cancelled) return;
        setKey(usage.key);
        setLogs(usage.data ?? []);
        if (page === 0) setDaily(dailyData ?? []);
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : 'Failed to load key usage');
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, page]);

  const chartData = useMemo(
    () =>
      [...daily]
        .sort((a, b) => a.date.localeCompare(b.date))
        .map((d) => ({
          date: d.date.slice(0, 10),
          cost: Number((d.total_cost_usd ?? 0).toFixed(4)),
          requests: d.request_count ?? 0,
          tokens: d.total_tokens ?? 0,
        })),
    [daily],
  );

  const totals = useMemo(() => {
    const totalCost = daily.reduce((s, d) => s + (d.total_cost_usd ?? 0), 0);
    const totalRequests = daily.reduce((s, d) => s + (d.request_count ?? 0), 0);
    const totalTokens = daily.reduce((s, d) => s + (d.total_tokens ?? 0), 0);
    return { totalCost, totalRequests, totalTokens };
  }, [daily]);

  const canPrev = page > 0;
  const canNext = logs.length === PAGE_SIZE;

  if (loading && !key) return <LoadingSkeleton rows={6} />;

  return (
    <div className="space-y-6">
      <div>
        <Link href="/keys" className="text-sm text-emerald-400 hover:text-emerald-300">
          ← All keys
        </Link>
      </div>
      <PageHeader
        title={key?.name ?? 'API Key'}
        description={
          key
            ? `Created ${new Date(key.created_at).toLocaleString()} · ${key.is_active ? 'active' : 'inactive'}`
            : undefined
        }
        action={
          key ? (
            <Badge variant={key.is_active ? 'success' : 'default'}>
              {key.is_active ? 'active' : 'inactive'}
            </Badge>
          ) : undefined
        }
      />

      {error && <Alert variant="error" onDismiss={() => setError(null)}>{error}</Alert>}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          title="Lifetime spend"
          value={key ? fmtUSD(key.total_spend, 4) : '—'}
          hint={key?.max_budget != null ? `Budget ${fmtUSD(key.max_budget, 2)}` : 'No budget set'}
        />
        <StatCard title="30-day spend" value={fmtUSD(totals.totalCost, 4)} hint={`${fmtNum(daily.length)} active days`} />
        <StatCard title="30-day requests" value={fmtNum(totals.totalRequests)} hint={`${fmtNum(totals.totalTokens)} tokens`} />
        <StatCard
          title="Allowed models"
          value={
            key?.models && key.models.length > 0
              ? key.models.includes('*')
                ? 'all'
                : String(key.models.length)
              : 'all'
          }
          hint={
            key?.models && key.models.length > 0 && !key.models.includes('*')
              ? key.models.join(', ')
              : 'No restrictions'
          }
        />
      </div>

      <div className="gatewayllm-card p-4">
        <h2 className="mb-3 text-sm font-semibold text-white">Spend over the last 30 days</h2>
        {chartData.length === 0 ? (
          <div className="py-12 text-center text-sm text-zinc-500">
            No spend recorded yet for this key. Send a request through it to see usage here.
          </div>
        ) : (
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="keySpendGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#10b981" stopOpacity={0.5} />
                    <stop offset="100%" stopColor="#10b981" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis dataKey="date" stroke="#71717a" tick={{ fontSize: 11 }} />
                <YAxis stroke="#71717a" tick={{ fontSize: 11 }} tickFormatter={(v) => `$${v}`} />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#0a0a0a',
                    border: '1px solid #27272a',
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  formatter={(v: number) => fmtUSD(v, 4)}
                />
                <Area
                  type="monotone"
                  dataKey="cost"
                  stroke="#10b981"
                  fill="url(#keySpendGrad)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </div>

      <div className="gatewayllm-table-wrap">
        <div className="flex items-center justify-between border-b border-zinc-800 bg-zinc-900/40 px-4 py-2">
          <span className="text-xs font-medium uppercase tracking-wide text-zinc-400">
            Recent requests
          </span>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-xs"
              disabled={!canPrev || loading}
              onClick={() => setPage((p) => Math.max(0, p - 1))}
            >
              Previous
            </button>
            <span className="text-xs text-zinc-500">Page {page + 1}</span>
            <button
              type="button"
              className="gatewayllm-btn-secondary py-1.5 text-xs"
              disabled={!canNext || loading}
              onClick={() => setPage((p) => p + 1)}
            >
              Next
            </button>
          </div>
        </div>
        <table className="gatewayllm-table text-xs sm:text-sm">
          <thead className="bg-zinc-900/40">
            <tr>
              <th className="gatewayllm-th">Time</th>
              <th className="gatewayllm-th">Model</th>
              <th className="gatewayllm-th">Provider</th>
              <th className="gatewayllm-th">Endpoint</th>
              <th className="gatewayllm-th text-right">Tokens</th>
              <th className="gatewayllm-th text-right">Latency</th>
              <th className="gatewayllm-th text-right">Cost</th>
              <th className="gatewayllm-th text-right">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-800 bg-zinc-950/40">
            {loading ? (
              <tr>
                <td colSpan={8} className="gatewayllm-td text-center text-zinc-500">
                  Loading…
                </td>
              </tr>
            ) : logs.length === 0 ? (
              <tr>
                <td colSpan={8} className="gatewayllm-td text-center text-zinc-500">
                  No requests recorded for this key yet.
                </td>
              </tr>
            ) : (
              logs.map((l) => (
                <tr key={l.id} className="hover:bg-zinc-900/40">
                  <td className="gatewayllm-td whitespace-nowrap text-zinc-400">
                    {new Date(l.created_at).toLocaleString()}
                  </td>
                  <td className="gatewayllm-td font-mono text-zinc-200">{l.model_alias}</td>
                  <td className="gatewayllm-td text-zinc-400">{l.provider}</td>
                  <td className="gatewayllm-td text-zinc-500">{l.endpoint}</td>
                  <td className="gatewayllm-td text-right tabular-nums text-zinc-300">
                    {fmtNum(l.total_tokens)}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-zinc-400">
                    {l.latency_ms != null ? `${fmtNum(l.latency_ms)} ms` : '—'}
                  </td>
                  <td className="gatewayllm-td text-right tabular-nums text-emerald-300">
                    {fmtUSD(l.cost_usd, 6)}
                  </td>
                  <td className="gatewayllm-td text-right">
                    <Badge
                      variant={
                        l.status_code >= 200 && l.status_code < 300
                          ? 'success'
                          : l.status_code >= 500
                          ? 'danger'
                          : 'default'
                      }
                    >
                      {l.status_code}
                    </Badge>
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
