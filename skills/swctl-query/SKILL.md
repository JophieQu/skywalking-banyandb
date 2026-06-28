---
name: swctl-query
description: >
  Use SkyWalking swctl and OAP GraphQL for high-level SkyWalking observability
  queries. Use when the user asks for swctl command construction, OAP entity
  discovery, trace or trace-v2 commands, profiling tasks, flame graphs,
  async-profiler, pprof, eBPF profiling, logs, metrics, admin commands,
  execution safety, or output handling.
---

# SkyWalking swctl Query

Use this skill to construct, explain, or execute SkyWalking `swctl` commands backed by OAP GraphQL and OAP admin APIs.

## Workflow

1. Prefer refreshing generated references before answering command-generation or "how do I query" questions. Use the local SkyWalking checkout when it exists:
   `python3 skills/swctl-query/scripts/extract_swctl_examples.py --skywalking-root /home/ququ/Programs/skywalking`
   If the local checkout is unavailable, use the GitHub fallback:
   `python3 skills/swctl-query/scripts/extract_swctl_examples.py --github-repo apache/skywalking --github-ref master --github-cache /tmp/skywalking-swctl-query-cache`
   Run `python3 skills/swctl-query/scripts/extract_swctl_commands.py` to refresh local command help, then run
   the examples generator for SkyWalking docs and e2e examples.
2. If network access, GitHub, or `swctl` is unavailable, do not silently treat stale references as latest. State the blocker, then continue from the most recent
   generated references and local `swctl --help` output.
3. Read `references/swctl.md` before constructing or executing commands.
4. Read `references/commands.md` when exact command names, aliases, or options are uncertain. It is generated from local `/usr/bin/swctl --help`.
5. Read `references/examples.md` when looking for SkyWalking docs and e2e command patterns.
6. Use `--base-url=http://localhost:12800/graphql` unless the user or environment provides another OAP GraphQL URL.
7. Use `--admin-url=http://localhost:17128` for any `admin` command unless the environment provides another admin URL.
8. For custom time ranges, pass both `--start` and `--end`; use `--end="0m"` for now.
9. If service, instance, endpoint, process, task, schedule, segment, or trace IDs are missing, run the OAP discovery commands first.
10. Combine this skill with `bydbql` or `skywalking-tables` when raw storage or schema context is useful for naming, ID decoding, or explaining results.
11. If execution is requested and inputs are known, run the command.
   If inputs are missing, run safe discovery commands or return the concrete command template with the missing inputs.
12. If `swctl` or OAP is unavailable, do not invent output; return the exact command and explain the blocker.

## Safety

Treat discovery, list, get, metrics, trace retrieval, logs, records, dependency, and health commands as safe read paths.
Do not execute mutating commands unless the user explicitly requests the mutation and provides the target environment. Mutating surfaces include
`create`, `update`, `delete`, `report`, `set`, `add`, `inactivate`, `disable`, profiling task creation, runtime-rule changes,
UI-template changes, event reporting, and session start/stop commands.

## Tool Selection

- Use `swctl-query` for SkyWalking command construction and OAP GraphQL or admin API execution.
- Use `bydbql` for raw BanyanDB stream, measure, trace, or property data.
- Use `skywalking-tables` for SkyWalking storage group names, metric/resource names, log fields, and entity ID conventions.

## Output

For command-generation requests, return the concrete `swctl` command and state that it was not executed.

For execution requests, show the command, summarize the returned data, and call out missing OAP inputs or unavailable services clearly.

## Refreshing References

Regenerate local command help with:

```bash
python3 skills/swctl-query/scripts/extract_swctl_commands.py
```

Regenerate examples from the preferred local SkyWalking checkout with:

```bash
python3 skills/swctl-query/scripts/extract_swctl_examples.py --skywalking-root /home/ququ/Programs/skywalking
```

When the local checkout is unavailable, use the GitHub fallback:

```bash
python3 skills/swctl-query/scripts/extract_swctl_examples.py --github-repo apache/skywalking --github-ref master --github-cache /tmp/skywalking-swctl-query-cache
```
