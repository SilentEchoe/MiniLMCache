# Upstream LMCache Source Map

Use this file when a MiniLMCache task should implement a simplified Go version of a real LMCache core module.

The purpose is not to clone LMCache. The purpose is to inspect the upstream behavior, identify the minimum teaching contract, then re-express that contract in a small, readable Go module.

## How To Use This Reference

For any core-module task:

1. Pick the MiniLMCache teaching target.
2. Open the matching upstream LMCache file(s) below.
3. Extract:
   - the module's role in the larger data flow
   - its main public or orchestration-facing methods
   - the state transitions or invariants that matter
4. Strip away production/distributed detail.
5. Rebuild the smallest equivalent teaching-oriented Go module.

Keep a written mapping in the implementation doc or PR summary:

- upstream module
- MiniLMCache module
- preserved behavior
- dropped behavior

## LOOKUP-Oriented Entry Points

### `lmcache/v1/multiprocess/server.py`

Use this as the top-level orchestration entry when explaining how a request enters the cache engine and gets routed through lookup-related paths.

Teaching value:

- shows the service-facing cache engine role
- useful when the MiniLMCache module should feel like an engine-facing facade

### `lmcache/v1/distributed/storage_manager.py`

Use this when modeling how lookup decisions affect later prefetch or retrieve actions.

Teaching value:

- central orchestration point for storage-facing flows
- useful for teaching that “cache hit” and “data ready for direct use” are not always the same thing

### `lmcache/v1/distributed/storage_controllers/prefetch_controller.py`

Use this when implementing or refining LOOKUP and prefix-hit behavior.

Teaching value:

- maps well to “ordered prefix reuse” and “what can be reused now”
- useful when reducing lookup into a minimal service/controller contract

## STORE-Oriented Entry Points

### `lmcache/v1/distributed/storage_controllers/store_controller.py`

Use this when implementing a teaching version of asynchronous store submission.

Teaching value:

- shows that store is a controlled pipeline, not just a map write
- useful for reduced demos of reserve/submit/complete style flows

## Retrieval / Storage Coordination

### `lmcache/v1/distributed/storage_manager.py`

Use this again for RETRIEVE and STORE coordination work, because the manager is the point where cache location, loading, and storage paths get composed.

Teaching value:

- helps define a minimal “manager” concept without copying the full LMCache architecture

## Reduction Rules

When translating upstream LMCache code into MiniLMCache:

- preserve behavior contracts, not class count
- preserve invariants, not concurrency topology
- preserve flow meaning, not storage backend variety
- prefer one Go package plus one in-memory implementation over layered infrastructure
- prefer trace output over hidden background behavior

## Acceptable Simplifications

These reductions are usually acceptable in MiniLMCache:

- replace distributed adapters with in-memory maps
- replace async worker pools with synchronous calls or explicit stubs
- replace background lifecycle controllers with simple state fields
- replace transport backends with location enums and trace messages
- replace complex error surfaces with a few explicit Go errors

## What Not To Lose

Even after simplification, do not lose these teaching points:

- ordered prefix semantics for reusable cache
- clear distinction between metadata hit and usable data availability
- separation between metadata plane and data plane
- the reason a later flow exists because of an earlier decision

## Useful Upstream Links

- LMCache repo: https://github.com/LMCache/LMCache
- Multiprocess server: https://raw.githubusercontent.com/LMCache/LMCache/dev/lmcache/v1/multiprocess/server.py
- Storage manager: https://raw.githubusercontent.com/LMCache/LMCache/dev/lmcache/v1/distributed/storage_manager.py
- Prefetch controller: https://raw.githubusercontent.com/LMCache/LMCache/dev/lmcache/v1/distributed/storage_controllers/prefetch_controller.py
- Store controller: https://raw.githubusercontent.com/LMCache/LMCache/dev/lmcache/v1/distributed/storage_controllers/store_controller.py
