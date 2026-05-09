'use client';

import { useParams } from 'next/navigation';
import { useMemo } from 'react';

import { SavingsDrillDown } from '@/components/SavingsDrillDown';
import { getSavingsByID } from '@/lib/api';

export default function SavingsDrillDownPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? '';

  const loader = useMemo(() => () => (id ? getSavingsByID(id) : Promise.resolve(null)), [id]);

  return (
    <SavingsDrillDown
      loader={loader}
      loaderKey={`id:${id}`}
      backHref="/savings"
      backLabel="All savings"
    />
  );
}
