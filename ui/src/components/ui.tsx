'use client';

import { useEffect, useState, type ReactNode } from 'react';

export function PageHeader({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight text-white">{title}</h1>
        {description && <p className="mt-1 text-sm text-zinc-400">{description}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

export function Alert({
  variant = 'error',
  children,
  onDismiss,
}: {
  variant?: 'error' | 'success' | 'warning' | 'info';
  children: ReactNode;
  onDismiss?: () => void;
}) {
  const styles = {
    error: 'border-red-800/50 bg-red-900/20 text-red-300',
    success: 'border-emerald-800/50 bg-emerald-900/20 text-emerald-300',
    warning: 'border-amber-800/50 bg-amber-950/30 text-amber-200',
    info: 'border-sky-800/50 bg-sky-900/20 text-sky-200',
  };
  return (
    <div className={`flex items-start gap-2 rounded-lg border px-4 py-3 text-sm ${styles[variant]}`}>
      <span className="flex-1">{children}</span>
      {onDismiss && (
        <button onClick={onDismiss} className="opacity-60 hover:opacity-100 transition-opacity">
          &times;
        </button>
      )}
    </div>
  );
}

export function LoadingSkeleton({ rows = 4, header = true }: { rows?: number; header?: boolean }) {
  return (
    <div className="space-y-6">
      {header && (
        <div>
          <div className="h-7 w-40 animate-pulse rounded bg-zinc-800" />
          <div className="mt-2 h-4 w-60 animate-pulse rounded bg-zinc-800/60" />
        </div>
      )}
      <div className="gatewayllm-card overflow-hidden">
        {Array.from({ length: rows }).map((_, i) => (
          <div
            key={i}
            className="flex items-center gap-4 border-b border-zinc-800/50 px-4 py-3.5 last:border-0"
          >
            <div className="h-4 flex-1 animate-pulse rounded bg-zinc-800" />
            <div className="h-4 w-24 animate-pulse rounded bg-zinc-800/60" />
            <div className="h-4 w-16 animate-pulse rounded bg-zinc-800/40" />
          </div>
        ))}
      </div>
    </div>
  );
}

export function EmptyState({
  message,
  action,
  actionLabel,
}: {
  message: string;
  action?: () => void;
  actionLabel?: string;
}) {
  return (
    <div className="px-4 py-12 text-center">
      <div className="mx-auto mb-3 h-10 w-10 rounded-full bg-zinc-800/60 flex items-center justify-center">
        <svg className="h-5 w-5 text-zinc-600" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
        </svg>
      </div>
      <p className="text-sm text-zinc-500">{message}</p>
      {action && actionLabel && (
        <button
          onClick={action}
          className="mt-2 text-sm text-emerald-400 hover:text-emerald-300 transition-colors"
        >
          {actionLabel}
        </button>
      )}
    </div>
  );
}

export function InfoTooltip({ text }: { text: string }) {
  const [show, setShow] = useState(false);
  return (
    <span className="relative inline-flex ml-1">
      <button
        type="button"
        onMouseEnter={() => setShow(true)}
        onMouseLeave={() => setShow(false)}
        onFocus={() => setShow(true)}
        onBlur={() => setShow(false)}
        className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-zinc-700/60 text-[10px] font-bold text-zinc-400 hover:bg-zinc-600/60 hover:text-zinc-200 transition-colors"
        aria-label="More info"
      >
        i
      </button>
      {show && (
        <div className="absolute bottom-full left-1/2 z-50 mb-2 w-64 -translate-x-1/2 rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-xs text-zinc-300 shadow-xl">
          {text}
          <div className="absolute top-full left-1/2 -translate-x-1/2 border-4 border-transparent border-t-zinc-700" />
        </div>
      )}
    </span>
  );
}

export function CopyButton({
  value,
  label,
  className,
}: {
  value: string;
  label?: string;
  className?: string;
}) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);

  async function handleClick() {
    try {
      if (typeof navigator !== 'undefined' && navigator.clipboard) {
        await navigator.clipboard.writeText(value);
      } else if (typeof document !== 'undefined') {
        const ta = document.createElement('textarea');
        ta.value = value;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      className={
        className ??
        'inline-flex items-center gap-1.5 rounded-md border border-zinc-700 bg-zinc-800/70 px-2.5 py-1 text-xs font-medium text-zinc-300 transition-colors hover:border-emerald-500/50 hover:bg-emerald-500/10 hover:text-emerald-300'
      }
      aria-label={label ?? 'Copy to clipboard'}
    >
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        {copied ? (
          <polyline points="20 6 9 17 4 12" />
        ) : (
          <>
            <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
          </>
        )}
      </svg>
      {copied ? 'Copied' : label ?? 'Copy'}
    </button>
  );
}

export function StatCard({
  title,
  value,
  hint,
  delta,
  icon,
}: {
  title: string;
  value: string;
  hint?: string;
  delta?: { label: string; direction: 'up' | 'down' | 'flat' };
  icon?: ReactNode;
}) {
  const deltaStyles = delta
    ? delta.direction === 'up'
      ? 'text-emerald-300 bg-emerald-500/10 border border-emerald-500/30'
      : delta.direction === 'down'
      ? 'text-red-300 bg-red-500/10 border border-red-500/30'
      : 'text-zinc-400 bg-zinc-800/80 border border-zinc-700'
    : '';

  const arrow = delta
    ? delta.direction === 'up'
      ? '\u2197'
      : delta.direction === 'down'
      ? '\u2198'
      : '\u2192'
    : '';

  return (
    <div className="gatewayllm-card p-5">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs font-medium uppercase tracking-wide text-zinc-500">{title}</p>
        {icon ? <span className="text-zinc-600">{icon}</span> : null}
      </div>
      <p className="mt-2 text-2xl font-semibold tabular-nums text-white">{value}</p>
      <div className="mt-1 flex flex-wrap items-center gap-2">
        {hint ? <p className="text-xs text-zinc-500">{hint}</p> : null}
        {delta ? (
          <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium ${deltaStyles}`}>
            <span aria-hidden>{arrow}</span>
            {delta.label}
          </span>
        ) : null}
      </div>
    </div>
  );
}

export function Badge({
  children,
  variant = 'default',
}: {
  children: ReactNode;
  variant?: 'default' | 'success' | 'danger' | 'warning';
}) {
  const styles = {
    default: 'bg-zinc-800 text-zinc-300',
    success: 'bg-emerald-900/40 text-emerald-300 ring-1 ring-emerald-700/50',
    danger: 'bg-red-900/40 text-red-300 ring-1 ring-red-700/50',
    warning: 'bg-amber-900/40 text-amber-300 ring-1 ring-amber-700/50',
  };
  return (
    <span className={`inline-block rounded-full px-2.5 py-0.5 text-xs font-medium ${styles[variant]}`}>
      {children}
    </span>
  );
}
