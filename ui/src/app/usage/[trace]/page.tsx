'use client';

import { useParams } from 'next/navigation';
import { useMemo } from 'react';

import { SavingsDrillDown } from '@/components/SavingsDrillDown';
import { getSavingsByTrace } from '@/lib/api';

// Drill-down hit from the /usage table. We intentionally reuse the
// signed-savings drill-down here: a usage row that happens to have a
// trace_id always also has a routing_savings row (see chat handler),
// so the same view renders the full baseline-vs-actual cost split
// plus quality + signature without needing a usage-specific schema.
export default function UsageDrillDownPage() {
  const params = useParams<{ trace: string }>();
  const trace = params?.trace ?? '';

  const loader = useMemo(
    () =>
      async () => {
        if (!trace) return null;
        try {
          return await getSavingsByTrace(trace);
        } catch (e) {
          // 404 just means there's no signed savings ledger row for this
          // request — surface as "not found" rather than a hard error.
          if (e instanceof Error && /not found|404/i.test(e.message)) return null;
          throw e;
        }
      },
    [trace]
  );

  return (
    <SavingsDrillDown
      loader={loader}
      loaderKey={`trace:${trace}`}
      backHref="/usage"
      backLabel="Back to usage"
    />
  );
}
