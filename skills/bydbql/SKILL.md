---
name: bydbql
description: Generate, validate, and optionally execute read-only BanyanDB BydbQL for metrics, logs, and properties. Use when the user asks to query BanyanDB, translate natural language to BydbQL, inspect BanyanDB schema or data, validate BydbQL, or route SkyWalking trace and profiling queries to swctl against OAP GraphQL instead of BydbQL.
---

# BanyanDB BydbQL

Use this skill when the user asks to query BanyanDB, generate BydbQL, translate natural language to BydbQL, inspect BanyanDB data, validate BydbQL, run read-only BanyanDB queries, or choose between BydbQL and swctl for logs, metrics, traces, profiling, or properties.

## Workflow

1. Read `references/safety.md` before generating, validating, or executing BydbQL.
2. Read `references/query-routing.md` before generating a query for SkyWalking logs, traces, profiling, or metrics, or when the user asks whether to use BydbQL or swctl.
3. Classify the request first by resource family, not by whether the user wants a UI-like or raw view:
   - Logs, metrics, and properties use BydbQL.
   - Traces and profiling use swctl against OAP GraphQL.
4. If the route is swctl, read `references/swctl.md`, use explicit `--start` and `--end` for non-default time windows, and use MCP only as supporting discovery when OAP entity names are missing, ambiguous, or rejected.
5. If the route is BydbQL, read `references/syntax.md` when query syntax is needed.
6. Read `references/examples.md` when mapping natural language to BydbQL.
7. If the group, resource name, or resource type is missing or ambiguous, use `list_groups_schemas`.
8. Generate exactly one read-only BydbQL statement when BydbQL is the right path.
9. Call `validate_bydbql` before execution.
10. Execute with `list_resources_bydbql` only when the user asks to run, query, fetch, show, or inspect raw BanyanDB data.
11. If `validate_bydbql` returns `valid=false`, fix the query and validate again before any execution.
12. If execution fails due to a missing group, resource, or schema, use `list_groups_schemas` to discover the correct names and retry only when the correction is clear.

## Tool Use

Use the existing BanyanDB MCP tools for BydbQL routes:

- `list_groups_schemas`: discover groups and resource names for streams, measures, traces, and properties.
- `validate_bydbql`: run read-only safety checks and parse-only BydbQL syntax validation.
- `list_resources_bydbql`: execute a validated BydbQL statement when data retrieval is requested.

Use terminal `swctl` commands for trace and profiling routes. Prefer concrete commands from `references/swctl.md`; do not rediscover basic time semantics by trial and error. Use BanyanDB MCP tools as supporting discovery for service, instance, endpoint, and trace-id names when `swctl` cannot resolve them, but do not replace trace or profiling retrieval with BydbQL. If execution is requested but `swctl` is unavailable, return the exact command and explain the blocker instead of inventing output.

Do not use `get_generate_bydbql_prompt` or `generate_BydbQL` as the primary path for this skill. The Codex host model should perform natural language to BydbQL translation using these references and live schema discovery.

## Output

For generation-only BydbQL requests, return the single BydbQL statement and state that it was not executed.

For BydbQL execution requests, show the BydbQL statement, then summarize the returned data or error concisely.

For swctl-routed trace or profiling requests, do not force a BydbQL query. Return or execute the concrete `swctl` command, then summarize the result or the missing inputs such as OAP URL, service name, endpoint name, task ID, or segment ID.
