#!/usr/bin/env node
// merge.mjs — collect every summarized *.json under the given dir (except
// merged output) into a single array for the website.
import fs from 'node:fs';
import path from 'node:path';
const dir = process.argv[2];
const files = fs.readdirSync(dir).filter((f) => f.endsWith('.json') && f !== 'all.json');
const entries = files.map((f) => JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')));
process.stdout.write(JSON.stringify({ run_at: new Date().toISOString(), results: entries }, null, 2));
