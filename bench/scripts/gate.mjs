#!/usr/bin/env node
// gate.mjs — enforce perf thresholds from bench/thresholds.yml against a
// benchstat-formatted comparison. Exits non-zero if any benchmark regresses
// beyond its allowed percentage OR exceeds its absolute p99 budget.
//
// benchstat output looks like:
//
//   name                     old time/op    new time/op    delta
//   Resolve-8                  450ns ± 2%     460ns ± 3%  +2.22%  (p=0.008 n=10+10)
//
// We parse the bench name, the delta percentage, and the "new" ns/op value.

import fs from 'node:fs';

const [thresholdPath, comparisonPath] = process.argv.slice(2);
const thresholds = parseYamlSimple(fs.readFileSync(thresholdPath, 'utf8'));
const text = fs.readFileSync(comparisonPath, 'utf8');

const rows = parseBenchstat(text);
let failed = 0;
for (const t of thresholds.benchmarks) {
  const row = rows.find((r) => r.name === t.name || r.name === `${t.name}-8` || r.name === `${t.name}-16`);
  if (!row) {
    console.warn(`! ${t.name}: not found in comparison`);
    continue;
  }
  const pct = row.deltaPct;
  const ns = row.newNs;
  let status = 'ok';
  if (ns != null && t.p99_ns != null && ns > t.p99_ns) {
    status = `FAIL p99 budget: ${ns.toFixed(0)}ns > ${t.p99_ns}ns`;
    failed++;
  } else if (pct != null && t.max_regression_pct != null && pct > t.max_regression_pct) {
    status = `FAIL regression: +${pct.toFixed(1)}% > ${t.max_regression_pct}%`;
    failed++;
  }
  console.log(`${status === 'ok' ? 'PASS' : 'FAIL'}  ${t.name}  new=${ns ?? '-'}ns  Δ=${pct?.toFixed(1) ?? '-'}%   ${status}`);
}
if (failed > 0) {
  console.error(`\n${failed} benchmark(s) failed the perf gate.`);
  process.exit(1);
}

function parseBenchstat(s) {
  const out = [];
  const lines = s.split('\n');
  for (const l of lines) {
    const m = l.match(/^(\S+)\s+\S+\s+(\d+(?:\.\d+)?)(?:n|µ|m)?s\/op.*?([+-]?\d+(?:\.\d+)?)%/);
    if (!m) continue;
    out.push({
      name: m[1].replace(/-\d+$/, ''),
      newNs: parseFloat(m[2]),
      deltaPct: parseFloat(m[3]),
    });
  }
  return out;
}

// Tiny YAML parser: we only need the subset our thresholds file uses.
function parseYamlSimple(s) {
  const out = { benchmarks: [] };
  let cur = null;
  for (const rawLine of s.split('\n')) {
    const line = rawLine.replace(/#.*$/, '').trimEnd();
    if (!line.trim()) continue;
    const itemMatch = line.match(/^  - name: (.+)$/);
    if (itemMatch) {
      if (cur) out.benchmarks.push(cur);
      cur = { name: itemMatch[1].trim() };
      continue;
    }
    const kvMatch = line.match(/^    (\w+): (.+)$/);
    if (kvMatch && cur) {
      const [, k, v] = kvMatch;
      const num = Number(v);
      cur[k] = Number.isNaN(num) ? v.trim() : num;
    }
  }
  if (cur) out.benchmarks.push(cur);
  return out;
}
