'use client';

import { useEffect, useState } from 'react';

interface Props {
  tipId: string;
  title: string;
  children: React.ReactNode;
}

export function FirstVisitTip({ tipId, title, children }: Props) {
  const storageKey = `gatewayllm_tip_${tipId}`;
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const seen = localStorage.getItem(storageKey);
    if (!seen) setVisible(true);
  }, [storageKey]);

  if (!visible) return null;

  function dismiss() {
    if (typeof window !== 'undefined') {
      localStorage.setItem(storageKey, 'true');
    }
    setVisible(false);
  }

  return (
    <div className="relative flex items-start gap-3 rounded-lg border border-emerald-900/50 bg-emerald-950/30 px-4 py-3 text-sm text-emerald-100">
      <span
        className="mt-1 flex h-2 w-2 shrink-0 items-center justify-center"
        aria-hidden
      >
        <span className="absolute inline-flex h-2 w-2 animate-ping rounded-full bg-emerald-400 opacity-75" />
        <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
      </span>
      <div className="flex-1">
        <p className="font-medium text-emerald-200">{title}</p>
        <p className="mt-1 text-emerald-100/80">{children}</p>
      </div>
      <button
        type="button"
        onClick={dismiss}
        className="text-xs text-emerald-300/70 hover:text-emerald-100"
        aria-label="Dismiss"
      >
        Got it
      </button>
    </div>
  );
}
