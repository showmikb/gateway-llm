'use client';

import {
  Area,
  Bar,
  CartesianGrid,
  ComposedChart,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import type { DailySpend } from '@/lib/types';

function formatDay(iso: string) {
  try {
    return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
  } catch {
    return iso;
  }
}

function dateKey(iso: string) {
  // Stable per-calendar-day key (YYYY-MM-DD) to avoid locale collisions when
  // grouping. The backend returns one row per (api_key, team, date), so we
  // need to aggregate before charting or duplicate labels appear on the axis.
  try {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toISOString().slice(0, 10);
  } catch {
    return iso;
  }
}

export function DashboardChart({ data }: { data: DailySpend[] }) {
  const grouped = new Map<string, { date: string; spend: number; requests: number }>();
  for (const d of data) {
    const key = dateKey(d.date);
    const existing = grouped.get(key);
    if (existing) {
      existing.spend += d.total_cost_usd;
      existing.requests += d.request_count;
    } else {
      grouped.set(key, {
        date: d.date,
        spend: d.total_cost_usd,
        requests: d.request_count,
      });
    }
  }
  const chartData = Array.from(grouped.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([, v]) => ({
      day: formatDay(v.date),
      spend: v.spend,
      requests: v.requests,
    }));

  if (chartData.length === 0) {
    return (
      <div className="flex h-[300px] items-center justify-center text-sm text-zinc-500">
        No spend data yet. Usage will appear after requests are routed through the gateway.
      </div>
    );
  }

  return (
    <ResponsiveContainer width="100%" height={300}>
      <ComposedChart data={chartData} margin={{ top: 8, right: 16, left: 0, bottom: 0 }}>
        <defs>
          <linearGradient id="spendFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#34d399" stopOpacity={0.35} />
            <stop offset="100%" stopColor="#34d399" stopOpacity={0} />
          </linearGradient>
        </defs>
        <CartesianGrid strokeDasharray="3 3" stroke="#27272a" vertical={false} />
        <XAxis dataKey="day" stroke="#71717a" tick={{ fontSize: 11 }} tickLine={false} axisLine={false} />
        <YAxis
          yAxisId="spend"
          stroke="#71717a"
          tick={{ fontSize: 11 }}
          tickLine={false}
          axisLine={false}
          tickFormatter={(v) => `$${Number(v).toFixed(2)}`}
        />
        <YAxis
          yAxisId="requests"
          orientation="right"
          stroke="#71717a"
          tick={{ fontSize: 11 }}
          tickLine={false}
          axisLine={false}
          tickFormatter={(v) => `${v}`}
        />
        <Tooltip
          contentStyle={{
            backgroundColor: '#18181b',
            border: '1px solid #3f3f46',
            borderRadius: '8px',
            fontSize: '12px',
          }}
          labelStyle={{ color: '#a1a1aa' }}
          formatter={(value: number, name) => {
            if (name === 'Spend') return [`$${Number(value).toFixed(4)}`, name];
            return [Number(value).toLocaleString(), name];
          }}
        />
        <Legend wrapperStyle={{ fontSize: 11, color: '#a1a1aa' }} />
        <Bar
          yAxisId="requests"
          dataKey="requests"
          name="Requests"
          fill="#10b981"
          fillOpacity={0.22}
          stroke="#059669"
          strokeOpacity={0.3}
          radius={[3, 3, 0, 0]}
          barSize={14}
        />
        <Area
          yAxisId="spend"
          type="monotone"
          dataKey="spend"
          name="Spend"
          stroke="#34d399"
          strokeWidth={2}
          fill="url(#spendFill)"
        />
      </ComposedChart>
    </ResponsiveContainer>
  );
}
