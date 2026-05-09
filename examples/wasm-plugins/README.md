# Gateway-LLM WASM Plugin SDK

Gateway-LLM's WASM plugin host (`backend/internal/wasmplugin`) runs
sandboxed extensions compiled from Rust, Go, TinyGo, AssemblyScript, or
any language that targets WebAssembly. Plugins can observe or mutate
every request that flows through the gateway without ever touching
core Go code.

## Hooks

| Hook             | When it fires                                 |
| ---------------- | --------------------------------------------- |
| `on_request`     | before the upstream provider is called        |
| `on_response`    | after a non-streaming response is received    |
| `on_stream_chunk`| for each SSE chunk (v0.2)                     |
| `on_error`       | when a provider returns an error              |
| `on_route`       | during alias resolution (v0.2)                |
| `on_eval`        | during replay/eval scoring (v0.2)             |

All hooks share the same ABI:

```wat
(func (export "on_request") (param i64 i64) (result i64))
```

* The two `i64` parameters are the pointer (first) and length (second)
  of a UTF-8 JSON blob inside the plugin's linear memory.
* The return value packs `(ptr << 32) | len`. Return `0` to signal
  "no mutation".
* The plugin must export `alloc(size: i32) -> i32` and
  `dealloc(ptr: i32, size: i32)` so the host can write input and read
  output safely.

## Host functions

Exposed from the `env` module:

* `gateway_log(level: i32, ptr: i32, len: i32)` — structured logging.
  Levels: `0=debug, 1=info, 2=warn, 3=error`.

Coming in v0.2 (gated by plugin options):

* `gateway_http_send` — outbound HTTP with allowlist.
* `gateway_kv_get` / `gateway_kv_set` — per-plugin persistent state.

## Configure a plugin

```yaml
plugins:
  enabled: true
  wasm:
    - name: langfuse-forwarder
      path: /etc/gateway-llm/plugins/langfuse.wasm
      call_timeout: 250ms
      options:
        endpoint: https://cloud.langfuse.com
```

## Rust starter (recommended)

```toml
# Cargo.toml
[lib]
crate-type = ["cdylib"]

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

```rust
// src/lib.rs
use std::slice;

#[no_mangle]
pub extern "C" fn alloc(size: u32) -> *mut u8 {
    let mut buf = Vec::with_capacity(size as usize);
    let p = buf.as_mut_ptr();
    std::mem::forget(buf);
    p
}

#[no_mangle]
pub unsafe extern "C" fn dealloc(ptr: *mut u8, size: u32) {
    let _ = Vec::from_raw_parts(ptr, 0, size as usize);
}

#[no_mangle]
pub unsafe extern "C" fn on_request(ptr: u64, len: u64) -> u64 {
    let slice = slice::from_raw_parts(ptr as *const u8, len as usize);
    let req: serde_json::Value = serde_json::from_slice(slice).unwrap();
    gateway_log(1, format!("request model={}", req["model"]).as_bytes());
    0 // no mutation
}

extern "C" {
    fn gateway_log(level: u32, ptr: *const u8, len: u32);
}

fn gateway_log_safe(level: u32, msg: &[u8]) {
    unsafe { gateway_log(level, msg.as_ptr(), msg.len() as u32) }
}
```

Build with:

```bash
cargo build --release --target wasm32-unknown-unknown
# produces target/wasm32-unknown-unknown/release/your_plugin.wasm
```

## Go (TinyGo) starter

```go
//go:build tinygo

package main

import (
    "encoding/json"
    "unsafe"
)

//export alloc
func alloc(size uint32) uintptr {
    buf := make([]byte, size)
    return uintptr(unsafe.Pointer(&buf[0]))
}

//export dealloc
func dealloc(ptr uintptr, size uint32) {}

//export on_request
func onRequest(ptr, length uint64) uint64 {
    raw := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(length))
    var req map[string]any
    _ = json.Unmarshal(raw, &req)
    return 0
}

func main() {}
```

Build with:

```bash
tinygo build -o plugin.wasm -target=wasi -no-debug ./main.go
```

## Security model

* Plugins run in an isolated linear memory with no filesystem or network
  access by default.
* Per-call CPU deadline (default 250ms). A misbehaving plugin is
  cancelled and the request continues as if the plugin had not run.
* Module memory is capped at 4 MiB by default.
* Core gateway-llm features (auth, rate limit, routing, privacy, cost)
  always run before any plugin sees a request — a plugin cannot bypass
  them.
