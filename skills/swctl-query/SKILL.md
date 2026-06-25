---
name: swctl-query
description: >
  Use SkyWalking swctl and OAP GraphQL for high-level SkyWalking observability
  queries. Use when the user asks for trace lists, spans, trace trees, profiling
  tasks, flame graphs, async-profiler, pprof, eBPF profiling, OAP entity
  discovery, or wants to reuse SkyWalking APIs instead of raw BanyanDB BydbQL.
---

# SkyWalking swctl Query

Use this skill when the final answer should come from SkyWalking OAP GraphQL through `swctl`, not from raw BanyanDB tables.
This is the default path for trace and profiling workflows because OAP owns entity resolution, trace shaping, profiling task state,
and UI-equivalent response logic.

## Workflow

1. Prefer refreshing generated references before answering command-generation or "how do I query" questions.
   Run `python3 skills/swctl-query/scripts/extract_swctl_commands.py` to refresh local command help, then run
   `python3 skills/swctl-query/scripts/extract_swctl_examples.py --github-repo apache/skywalking --github-ref master --github-cache /tmp/skywalking-swctl-query-cache`
   to refresh SkyWalking GitHub docs and e2e examples.
2. If network access, GitHub, or `swctl` is unavailable, do not silently treat stale references as latest. State the blocker, then continue from the most recent
   generated references and local `swctl --help` output.
3. Read `references/swctl.md` before constructing or executing commands.
4. Read `references/commands.md` when exact command names, aliases, or options are uncertain. It is generated from local `/usr/bin/swctl --help`.
5. Read `references/examples.md` when looking for SkyWalking GitHub docs and e2e command patterns.
6. Use `--base-url=http://localhost:12800/graphql` unless the user or environment provides another OAP GraphQL URL.
7. Use `--admin-url=http://localhost:17128` for any `admin` command unless the environment provides another admin URL.
8. For custom time ranges, pass both `--start` and `--end`; use `--end="0m"` for now.
9. For trace and profiling requests, keep the final retrieval or analysis command on `swctl`.
10. If service, instance, endpoint, process, task, schedule, segment, or trace IDs are missing, run the OAP discovery commands first.
11. Use BanyanDB MCP and the `skywalking-tables` or `bydbql` skills only as auxiliary discovery when OAP names are missing, ambiguous, or rejected.
   Do not replace trace or profiling results with raw BydbQL.
12. If execution is requested and inputs are known, run the command.
   If inputs are missing, run safe discovery commands or return the concrete command template with the missing inputs.
13. If `swctl` or OAP is unavailable, do not invent output; return the exact command and explain the blocker.

## Safety

Treat discovery, list, get, metrics, trace retrieval, logs, records, dependency, and health commands as safe read paths.
Do not execute mutating commands unless the user explicitly requests the mutation and provides the target environment. Mutating surfaces include
`create`, `update`, `delete`, `report`, `set`, `add`, `inactivate`, `disable`, profiling task creation, runtime-rule changes,
UI-template changes, event reporting, and session start/stop commands.

## Tool Selection

- Use `swctl-query` for SkyWalking trace, span, profiling, flame graph, process, dependency, and UI-equivalent OAP queries.
- Use `bydbql` for raw BanyanDB stream, measure, trace, or property data.
- Use `skywalking-tables` when either path needs SkyWalking storage group names, metric/resource names, log fields, or entity ID conventions.

## Output

For command-generation requests, return the concrete `swctl` command and state that it was not executed.

For execution requests, show the command, summarize the returned data, and call out missing OAP inputs or unavailable services clearly.

## Refreshing References

Regenerate local command help with:

```bash
python3 skills/swctl-query/scripts/extract_swctl_commands.py
```

Regenerate GitHub-derived examples with:

```bash
python3 skills/swctl-query/scripts/extract_swctl_examples.py --github-repo apache/skywalking --github-ref master --github-cache /tmp/skywalking-swctl-query-cache
```

Pass `--skywalking-root <path>` only when intentionally testing against a local checkout. For latest upstream docs and e2e patterns, omit
`--skywalking-root` so the script downloads the requested GitHub ref.
