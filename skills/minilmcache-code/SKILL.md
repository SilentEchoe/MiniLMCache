---
name: minilmcache-code
description: Use for MiniLMCache repository tasks involving educational Go implementations, refactors, reviews, tests, demos, or documentation around LMCache-style LOOKUP/RETRIEVE/STORE flows, chunking, metadata-plane vs data-plane separation, and cache reuse behavior.
---

# MiniLMCache Code

## Overview

Use this skill when working in the `MiniLMCache` repository. Keep the project teaching-first: readable, minimal, observable, and easy to trace. Favor small Go implementations, in-memory components, and explicit docs/tests over production-style abstractions.

## Upstream-First Workflow

Before implementing a new MiniLMCache core module, inspect the corresponding LMCache upstream code first. Do not invent the module shape from scratch if the goal is to teach a simplified version of a real LMCache behavior.

Use this sequence:

1. Read the local architecture docs already present in the repo.
2. Read the relevant LMCache upstream source entrypoints listed in `references/upstream-lmcache.md`.
3. Extract the smallest stable teaching contract:
   - what problem the upstream module solves
   - what inputs and outputs matter
   - what invariants must remain true
   - which parts are implementation detail and can be removed
4. Implement a reduced Go version in MiniLMCache.
5. Document the mapping:
   - upstream module(s)
   - retained behavior
   - intentionally omitted behavior
   - why the simplification is acceptable for teaching

Never mechanically port the Python structure into Go. Preserve the behavior contract, then redesign the implementation to stay small and readable.

## Core Working Model

- Treat `MiniLMCache` as a teaching project, not a production cache system.
- Preserve the separation between:
  - `metadata plane`: chunk registration, lookup, ownership, location mapping
  - `data plane`: chunk bytes, transport, retrieve/store paths
- Prefer deterministic behavior:
  - stable key generation
  - repeatable tests
  - observable hit/miss/retrieve/store traces
- Default to the evolution order:
  - `LOOKUP`
  - `RETRIEVE`
  - `STORE`
  Only change that order if the user explicitly asks.
- Default to “one upstream core module -> one simplified MiniLMCache Go module” as the teaching unit.

## Context Build Order

Before editing, read only the files needed for the current task in this order:

1. `README.md`
2. `AGENT.md`
3. Relevant docs under `docs/`
   - architecture work: `docs/lmcache-architecture.md`
   - LOOKUP teaching/demo work: `docs/lookup-demo.md`
4. The relevant upstream LMCache references in `references/upstream-lmcache.md`
5. The specific Go package or command being changed

Do not assume the repo is larger than it is. Build context from the current tree first.

## Implementation Rules

- Use Go standard library first.
- Prefer package-level clarity over framework-like layering.
- Keep names aligned with cache-reuse semantics, not model-framework internals.
- Use in-memory or mock implementations before introducing persistence, remote systems, or concurrency-heavy designs.
- Make each data flow observable with one or more of:
  - trace events
  - CLI demo output
  - focused tests
- When adding new behavior that changes how the teaching story is told, update docs in the same task.
- When implementing or refactoring a core module, explicitly state which upstream LMCache module(s) it is derived from.
- Prefer behavior reduction over architecture imitation:
  - collapse async workers into direct calls or small stubs
  - replace distributed storage with in-memory adapters
  - replace complex lifecycle control with explicit traceable state
  - keep names close enough that readers can map back to upstream concepts
- Do not add production features unless explicitly required:
  - real tensor storage
  - GPU memory management
  - vLLM/SGLang integration
  - high-performance networking
  - production-grade consistency, locking, or eviction

## Delivery Expectations

For code changes, prefer delivering all of the following in one pass when feasible:

- implementation
- tests or reproducible verification
- demo updates when behavior should be visible
- documentation synchronization
- upstream mapping notes for the changed module when the implementation is based on LMCache source

If behavior or public types change, update the relevant docs:

- `README.md` for project-level orientation
- `AGENT.md` for collaborator guidance
- the corresponding file under `docs/` for teaching walkthroughs

For core-module work, include a short note in the relevant doc that answers:

- which upstream file(s) were inspected
- what behavior was preserved
- what was intentionally dropped or stubbed

## Testing And Verification

- Run `gofmt -w` on changed Go files.
- Run `go test ./...` after meaningful code changes.
- If a CLI demo is relevant, run it as part of verification.
- In sandboxed environments, prefer a repo-local Go cache for demo execution:

```bash
env GOCACHE=$PWD/.gocache go run ./cmd/...
```

- When adding logic, cover at least:
  - happy path
  - miss or failure path
  - one edge case that reflects the teaching contract

## Feature-Specific Guidance

### LOOKUP

- Treat lookup as a decision phase, not a plain key-value read.
- Make clear which prefix tokens are reusable and where missing computation begins.
- Preserve ordered prefix semantics; do not reuse later chunks if an earlier chunk misses.
- When possible, derive LOOKUP behavior from upstream LMCache lookup/prefetch paths, then reduce it into a small Go service and controller pair.

### RETRIEVE

- Model retrieve as “known reusable data that still needs to be brought into a usable location”.
- Keep the boundary between metadata hit and actual data availability explicit.
- Reduce upstream retrieve/load flows into explicit state transitions readers can follow in one sitting.

### STORE

- Keep store off the critical teaching path when possible.
- Emphasize that write-back can be asynchronous even in a minimal demo.
- Reduce upstream store pipelines into the smallest form that still demonstrates reserve, submit, and async completion semantics when those matter.

## Communication

- Prefer Chinese for user-facing explanations unless the user asks for English.
- Keep explanations concrete and repo-specific.
- When reviewing code, prioritize behavioral risks, missing tests, and teaching-story regressions over style nitpicks.
