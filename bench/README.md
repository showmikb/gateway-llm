# gateway-llm benchmarks

Every performance claim we make publicly is backed by a script in this
directory. If a benchmark lives here, we publish its results. If it doesn't,
we don't make the claim.

## What we benchmark

| Layer         | Tool                 | What it answers                                   |
| ------------- | -------------------- | ------------------------------------------------- |
| Go microbench | `go test -bench`     | Cost of routing, IR convert, redact, receipt sign |
| k6 end-to-end | `k6 run`             | p50 / p99 latency + throughput under load         |
| Mock upstream | `mockllm/`           | Subtract upstream variance so we measure *us*     |
| Comparative   | `scripts/compare.sh` | Same traffic against LiteLLM / Bifrost / Portkey  |

## Apples-to-apples guarantee

Every comparative benchmark is run with:

- The same prompt set (`data/prompts.jsonl`)
- The same mock upstream (`mockllm`) with a fixed 50ms response time
- The same hardware (GitHub-hosted `ubuntu-latest`, pinned to the runner OS
  image commit)
- The same concurrency (`-u 50` virtual users for 60 seconds)

This removes every variable except the proxy. You can reproduce a run end-to-
end with:

```bash
./scripts/run_all.sh
```

## CI perf gate

`.github/workflows/perf.yml` runs `go test -bench` on every PR against main.
If any of these regress beyond the configured thresholds, CI fails:

| Benchmark                        | p99 threshold | % max regression |
| -------------------------------- | ------------- | ---------------- |
| `BenchmarkRouterResolve`         | 1 µs          | 10%              |
| `BenchmarkIRFromOpenAI`          | 5 µs          | 10%              |
| `BenchmarkReceiptSign`           | 200 µs        | 15%              |
| `BenchmarkBlobPut` (memory)      | 20 µs         | 10%              |

Thresholds live in `bench/thresholds.yml`. Bumping them requires a PR titled
`perf: relax <benchmark> threshold — reason` so every regression is visible
in git history.
