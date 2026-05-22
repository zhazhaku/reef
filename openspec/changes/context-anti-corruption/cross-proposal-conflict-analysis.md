# Cross-Proposal Conflict Analysis: context-sandbox vs All Proposals

> date: 2026-05-11
> scope: `/root/reef_server/.reef/workspace/picoclaw/openspec/changes/*`
> conclusion: **NO CONFLICTS. context-sandbox is complementary infrastructure for all other proposals.**

---

## 1. Full Proposal Inventory

| # | Proposal | Status | Focus Area |
|---|----------|--------|------------|
| 1 | `deepseek-token-optimization` | ✅ Implemented | P1-P5: prompt partitioning, dynamic relocation, tool/reasoning truncation, token probe |
| 2 | `context-sandbox` | 📋 Proposed | L1-L4: Think-in-Code routing, checkpoint, sandbox execution, recursive reflector |
| 3 | `reef-hermes-architecture` | 🔬 Research | HermesMode coordinator/executor/full, multi-phase workflow, shared task board |
| 4 | `reef-scheduler-v2` | 📐 Planning | Server-centric scheduling, DAG engine, persistent queue, GatewayBridge |
| 5 | `reef-rebrand` | 📋 Proposed | picoclaw → reef rename (strings, env vars, module path) |
| 6 | `reef-v2.0` | 📋 Proposed | Persistent queue, Web UI, TLS, multi-channel alerts |

---

## 2. Per-Proposal Conflict Check

### 2.1 vs `deepseek-token-optimization` — UPSTREAM/DOWNSTREAM

**Relationship**: P3 (tool truncation) → L3 (sandbox execution) is an evolutionary path.

| deepseek-token-opt | context-sandbox | Relationship |
|---|---|---|
| P3: truncate tool output to 2-4K chars | L3: route tool output to seahorse, only summary in context | L3 is the next step after P3 |
| P4: reasoning 4096-char cap | L4: Reflector captures learnings from successful/failed reasoning | Complementary |
| P1: stable prefix partitioning | L1: Think-in-Code prompt rule | No interaction |

**Shared file**: `context.go`. deepseek changed `BuildMessagesFromPrompt` (lines 666-800); sandbox L1 changes `getIdentity()` (adds one prompt rule). Different functions, no overlap.

**No conflict.**

### 2.2 vs `reef-hermes-architecture` — INFRASTRUCTURE RELATIONSHIP

**Key finding**: The `research-context-rot.md` file (created 2026-05-11, same directory) independently diagnosed the exact problems context-sandbox solves:

```
Real production data from gateway.log turn-22:
  reasoning_tokens: 57,195 (41%)    → P4 solves this
  tool_result_chars: 144,501        → L3 solves this  
  summary_tokens: 0 (never triggers) → L2 solves this
  history_count: 559 messages       → seahorse under pressure
```

The Hermes workflow's **shared task board** creates massive context pressure:

```
Brainstorming (4 agents × 5 rounds):
  Without sandbox: 100K+ tokens per agent (other agents' reasoning + tool outputs)
  With sandbox:    20K tokens per agent (summaries only)
```

**Context-sandbox is a prerequisite for the multi-agent workflow to scale beyond 3 rounds.**

HermesGuard (Layer 3 in reef-hermes-architecture) prevents Server from executing tools directly — this is a **different concern** from context-sandbox. HermesGuard controls **who** can execute tools; context-sandbox controls **how** tool output enters context.

**No conflict. Synergy: context-sandbox makes the shared task board viable.**

### 2.3 vs `reef-scheduler-v2` — ORTHOGONAL

| reef-scheduler-v2 concern | context-sandbox concern |
|---|---|
| Server-centric task dispatch | Per-client context health |
| DAG dependency management | Tool output isolation |
| Priority queue scheduling | Compression-time checkpoint |
| Persistent TaskStore | seahorse-based memory |

**Only alignment point**: reef-scheduler-v2 Phase 0 changes `GetHome()` priority (`REEF_HOME > exe_dir > ~/.reef`). context-sandbox doesn't hardcode paths — it uses seahorse interfaces. No impact.

**No conflict.**

### 2.4 vs `reef-rebrand` — ZERO IMPACT

Rebrand is purely string replacements (picoclaw → reef, PICOCLAW_HOME → REEF_HOME). context-sandbox references Go imports, not literal strings — imports will be renamed atomically by the rebrand process.

**No conflict.**

### 2.5 vs `reef-v2.0` — ORTHOGONAL

reef-v2.0 adds persistence, WebUI, TLS, alert channels — all at the infrastructure/serving layer. context-sandbox operates at the agent reasoning layer. Different abstraction levels entirely.

**No conflict.**

---

## 3. Shared Files Check

Only one file is touched by multiple proposals:

| File | deepseek-token-opt | context-sandbox | reef-hermes | Conflict? |
|------|:--:|:--:|:--:|:--:|
| `pkg/agent/context.go` | `BuildMessagesFromPrompt` (L666-800) | `getIdentity()` (L1 prompt rule) | (not yet implemented) | **No** — different functions |

All other files are add-only (new files) or exclusive to a single proposal.

---

## 4. Architectural Layer Map

```
┌──────────────────────────────────────────────────────────────┐
│  Infrastructure Layer (reef-v2.0, reef-rebrand)               │
│  TLS, WebUI, persistent queue, alerts, naming                │
├──────────────────────────────────────────────────────────────┤
│  Coordination Layer (reef-scheduler-v2, reef-hermes-arch)     │
│  Server scheduling, DAG, HermesMode, workflow phases          │
├──────────────────────────────────────────────────────────────┤
│  Context Health Layer (context-sandbox)          ← NEW LAYER  │
│  L1 Think-in-Code, L2 Checkpoint, L3 Sandbox, L4 Reflector   │
├──────────────────────────────────────────────────────────────┤
│  Token Optimization Layer (deepseek-token-optimization)       │
│  P1-P5: partition, relocate, truncate, probe                 │
├──────────────────────────────────────────────────────────────┤
│  Memory Layer (seahorse + JSONL + MEMORY.md)                  │
│  Full-fidelity storage, FTS5 search, budget-aware assembly    │
└──────────────────────────────────────────────────────────────┘
```

Context-sandbox fills the gap between token optimization (reactive reduction) and coordination (workflow orchestration) — it provides the **proactive hygiene** that prevents context rot before token budgets are exhausted.

---

## 5. Conclusion

| Assertion | Evidence |
|-----------|----------|
| **No conflicts with any existing proposal** | Six proposals touch different files/functions/layers |
| **context-sandbox is prerequisite for Hermes workflow at scale** | `research-context-rot.md` data shows 100K+ token pressure in shared task board without sandbox |
| **L3 sandbox gives maximum benefit in multi-agent scenarios** | Each agent's context is polluted by other agents' raw tool outputs |
| **Only shared file (`context.go`) has non-overlapping changes** | deepseek → `BuildMessagesFromPrompt`, sandbox → `getIdentity()` |
| **Context-sandbox fills a layer gap** | Between token optimization (reactive) and coordination (orchestration) |
