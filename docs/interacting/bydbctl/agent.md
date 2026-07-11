# BYDBQL Agent TUI

`bydbctl agent` is a three-page terminal workspace where an ACP agent discovers BanyanDB schemas, proposes typed query plans, and safely runs approved queries.

The default provider is `codex-acp`. It starts `@agentclientprotocol/codex-acp` through `npx`; a different ACP-compatible stdio command can be selected with `--agent acp --acp-command …`.

## Start

```shell
bydbctl agent \
  --addr http://localhost:17913 \
  --goal "top slow payment endpoints in the last 30 minutes"
```

For a custom ACP provider:

```shell
bydbctl agent \
  --agent acp \
  --acp-command npx \
  --acp-arg -y \
  --acp-arg @agentclientprotocol/claude-agent-acp \
  --addr https://banyandb.example:17913 \
  --enable-tls \
  --cert /path/to/ca.pem
```

The Agent TUI uses the same `--addr`, username/password, TLS certificate, and `--insecure` semantics as the normal bydbctl HTTP commands. The external provider never receives those settings or BanyanDB credentials.

## Controlled tools and safety

Each TUI session creates a private, local MCP bridge. It exposes exactly these tools:

- `list_groups_schemas`
- `describe_schema`
- `propose_query_plan`
- `validate_bydbql`
- `execute_bydbql`

The Agent starts with no selected schema. It ranks catalog candidates, inspects at most three detailed schemas, and selects a resource only when its typed metadata supports the request. If the best choices remain ambiguous, it asks one focused clarification question. The Schema page is read-only and cannot pin a resource.

`propose_query_plan` accepts a strict JSON plan and uses typed local metadata to render final BYDBQL. The planner supports projections, comparison/`IN` filters with `AND`/`OR`, indexed ordering, Measure aggregation/grouping, and `SHOW TOP`. It rejects `MATCH`, `HAVING`, `OFFSET`, `STAGES`, `WITH QUERY_TRACE`, joins, and unknown column types rather than guessing. `validate_bydbql` remains for manual editor checks; it cannot publish a provider candidate. The bridge rejects every other tool, shell command, external MCP server, dynamic registration, and download.

The legacy `--mcp-config` option is retained only to produce an explicit error; external MCP injection is not supported. The old `codex-exec` provider is removed. The deterministic `fake` provider is test-only and is not a CLI provider. ACP providers that cannot use the controlled plan tool fail instead of falling back to free-text BYDBQL.

## Execution approval

No query runs merely because the agent generated it. `execute_bydbql` and the manual `Ctrl+E` path create an approval card containing the exact BYDBQL statement, resource, groups, time range, and limit.

- `y` approves that exact statement for one request.
- `n` rejects it.
- `e` rejects it, stops the active turn, and copies the statement into the editor for revision.

Any changed statement requires a new approval. The card also shows the effective query timeout and the fixed 50-row local preview bound. Immediately after approval, bydbctl validates the exact statement again; failed revalidation prevents execution. A failed execution never retries automatically, but its sanitized feedback can produce a new, separately approved plan. `Esc` or `Ctrl+C` stops an active agent/query operation, rejects pending approvals, and retains the activity and candidate history.

The local semantic checks require a `TIME` clause for time-series queries and a `LIMIT` for `SELECT` queries. These checks complement BanyanDB execution and do not grant the provider permission to access data.

## Pages and controls

| Page | Key | Purpose |
| --- | --- | --- |
| Schema | F1 | Review catalog candidates, inspected schemas, typed columns, and the Agent's selected evidence |
| Query / Agent | F2 | Multi-turn conversation, current BYDBQL version, validation and approval |
| Run / Activity | F3 | Structured result preview and live plan/tool/approval/execution activity |

Tab navigation works globally, including while typing in Query inputs:

| Shortcut | Action |
| --- | --- |
| `F1` / `F2` / `F3` | Jump directly to Schema / Query / Run |
| `[` / `]` | Previous / next tab (works even while typing) |
| `Ctrl+]` | Next tab (works even while typing) |

On the Query page:

| Shortcut | Action |
| --- | --- |
| `Ctrl+A` | Send the overall goal plus the optional next instruction to the ACP agent |
| `Ctrl+V` | Validate the current editor content immediately |
| `Ctrl+E` | Request one-time execution approval for the current valid content |
| `Ctrl+←` / `Ctrl+→` | Select a previous or next BYDBQL candidate version |
| `Tab` / `Shift+Tab` | Change focus |
| `Esc` / `Ctrl+C` | Stop active work; quit when idle |

Editing the query creates a manual candidate. The editor performs a short debounced local validation but never invokes the agent or runs a query automatically. Agent and manual candidates are versioned independently; a later agent turn starts from the selected version.

## Results and data sharing

The Run page shows resource type, duration, row count, and a bounded structured table preview. The raw HTTP response remains available only in the current process as a detail view. It is neither sent to the ACP provider nor written to the normal session log.

When a user asks a later question, or when a workflow advances to a dependent planned query, bydbctl supplies the provider the current statement, result type, row count, duration, column summary, error, and up to 50 preview rows. No extra sharing option is required. A multi-resource goal is represented as multiple independently compiled and approved queries; BanyanDB joins are never fabricated.

## Activity log and persistence

The activity log shows user-visible plans, tool lifecycle states, approval decisions, validation, cancellation, and execution summaries. It never displays model internal reasoning.

Session logs are stored in `$HOME/.bydbctl/logs` by default (override with `--log-dir`) with owner-only file permissions. They contain audit summaries: user actions, candidate statements, tool/approval summaries, durations, row counts, and errors. Raw result rows and long provider responses stay in memory and are not persisted. Sessions end when the TUI exits; cross-process recovery is not implemented.

## Troubleshooting

- If ACP cannot start, check the selected ACP command and its local login/runtime prerequisites. `codex-acp` requires `npx` and the Codex ACP package.
- If schema discovery fails, verify the normal bydbctl address, authentication, TLS, certificate, and server permissions.
- If no candidate appears, inspect the Activity page. The provider must call `propose_query_plan`; a BYDBQL statement embedded in chat text is intentionally ignored.
- If an approval fails after `y`, review the local revalidation error, update the query, and request approval again.
