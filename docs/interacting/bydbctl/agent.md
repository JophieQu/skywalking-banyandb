# BYDBQL Agent TUI

`bydbctl agent` is a three-page terminal workspace for discovering BanyanDB schemas, drafting BYDBQL with an ACP agent, and safely running approved queries.

The default provider is `codex-acp`. It starts `@agentclientprotocol/codex-acp` through `npx`; a different ACP-compatible stdio command can be selected with `--agent acp --acp-command …`.

## Start

```shell
bydbctl agent \
  --addr http://localhost:17913 \
  --goal "top slow payment endpoints in the last 30 minutes" \
  --groups sw_metrics \
  --resource-type MEASURE \
  --name service_endpoint_latency
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
- `validate_bydbql`
- `execute_bydbql`

Schema discovery and validation can run automatically. The bridge rejects every other tool, shell command, external MCP server, dynamic registration, and download. The legacy `--mcp-config` option is retained only to produce an explicit error; external MCP injection is not supported. The old `codex-exec` provider is removed. The deterministic `fake` provider is test-only and is not a CLI provider.

An agent must publish a candidate through the structured `validate_bydbql` tool call. bydbctl deliberately does not extract a statement from Markdown, JSON, or free-form provider text.

## Execution approval

No query runs merely because the agent generated it. `execute_bydbql` and the manual `Ctrl+E` path create an approval card containing the exact BYDBQL statement, resource, groups, time range, and limit.

- `y` approves that exact statement for one request.
- `n` rejects it.
- `e` rejects it, stops the active turn, and copies the statement into the editor for revision.

Any changed statement requires a new approval. The card also shows the effective query timeout and local preview bound. Configure these with `--query-timeout` and `--preview-rows`. Immediately after approval, bydbctl validates the exact statement again; failed revalidation prevents execution. Queries are never retried automatically. `Esc` or `Ctrl+C` stops an active agent/query operation, rejects pending approvals, and retains the activity and candidate history.

The local semantic checks require a `TIME` clause for time-series queries and a `LIMIT` for `SELECT` queries. These checks complement BanyanDB execution and do not grant the provider permission to access data.

## Pages and controls

| Page | Key | Purpose |
| --- | --- | --- |
| Schema | F1 | Browse groups and resources, inspect tags/fields/indexes, select a resource |
| Query / Agent | F2 | Multi-turn conversation, current BYDBQL version, validation and approval |
| Run / Activity | F3 | Structured result preview and live plan/tool/approval/execution activity |

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

When a user asks a later question, bydbctl supplies the provider only the current statement, result type, row count, duration, column summary, and error. It does not automatically trigger a follow-up agent turn after execution. Press `Ctrl+P` to explicitly attach a bounded subset of the current table preview to the next agent turn; press it again to turn sharing off.

## Activity log and persistence

The activity log shows user-visible plans, tool lifecycle states, approval decisions, validation, cancellation, and execution summaries. It never displays model internal reasoning.

Session logs are stored in `$HOME/.bydbctl/logs` by default (override with `--log-dir`) with owner-only file permissions. They contain audit summaries: user actions, candidate statements, tool/approval summaries, durations, row counts, and errors. Raw result rows and long provider responses stay in memory and are not persisted. Sessions end when the TUI exits; cross-process recovery is not implemented.

## Troubleshooting

- If ACP cannot start, check the selected ACP command and its local login/runtime prerequisites. `codex-acp` requires `npx` and the Codex ACP package.
- If schema discovery fails, verify the normal bydbctl address, authentication, TLS, certificate, and server permissions.
- If no candidate appears, inspect the Activity page. The provider must call `validate_bydbql`; a statement embedded in chat text is intentionally ignored.
- If an approval fails after `y`, review the local revalidation error, update the query, and request approval again.
