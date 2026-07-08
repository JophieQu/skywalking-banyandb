# BYDBQL Agent TUI

`bydbctl agent` opens an interactive TUI for building, editing, validating, executing, and accepting BYDBQL queries.

The agent can generate and revise BYDBQL, but it does not execute shell commands or query BanyanDB directly. `bydbctl` owns schema discovery,
validation, execution, and final acceptance.

## Start the TUI

Use the built-in deterministic demo agent:

```shell
bydbctl agent \
  --addr http://localhost:17913 \
  --goal "top slow payment endpoints in the last 30 minutes" \
  --groups sw_metrics \
  --resource-type MEASURE \
  --name service_endpoint_latency
```

Use Codex through `codex exec`:

```shell
bydbctl agent \
  --agent codex-exec \
  --addr http://localhost:17913 \
  --goal "top slow payment endpoints in the last 30 minutes" \
  --groups sw_metrics \
  --resource-type MEASURE \
  --name service_endpoint_latency
```

Use Codex through `agentclientprotocol/codex-acp`:

```shell
bydbctl agent \
  --agent codex-acp \
  --mcp-config .mcp.json \
  --addr http://localhost:17913 \
  --goal "top slow payment endpoints in the last 30 minutes" \
  --groups sw_metrics \
  --resource-type MEASURE \
  --name service_endpoint_latency
```

`--agent codex-acp` starts this ACP server internally:

```shell
npx -y @agentclientprotocol/codex-acp
```

For Codex ACP with BanyanDB MCP tools, build the MCP server artifacts first:

```shell
make -C mcp build
make -C mcp build:validator
```

`--mcp-config .mcp.json` passes the configured BanyanDB MCP server to the ACP session. The default is empty because some ACP servers reject
`mcpServers` in `session/new`. Pass the flag only when your ACP adapter supports MCP server injection.

The BanyanDB MCP server exposes tools such as `list_groups_schemas`, `validate_bydbql`, and `list_resources_bydbql`. Codex can use those read-only
tools while drafting or checking a query. The final TUI flow still writes the BYDBQL into the editor, and `Ctrl+E` remains the bydbctl-owned execution
gate shown in `Execution Preview`.

You can also pass a custom ACP-compatible command:

```shell
bydbctl agent \
  --agent acp \
  --acp-command npx \
  --acp-arg -y \
  --acp-arg @agentclientprotocol/codex-acp
```

## Screen Layout

The left side contains:

- `Goal`: the natural language question for the agent.
- `Slots`: resource type, resource name, groups, and time range.
- `Workflow`: the current workflow phase.
- `Events`: short workflow hints; full details are written to a session log file shown at the bottom of the panel.

Each `bydbctl agent` run writes a timestamped log under `$HOME/.bydbctl/logs/agent-YYYYMMDD-HHMMSS.log`. Override the directory with `--log-dir`. When the TUI exits, the full log path is printed to stderr.

The right side contains:

- `BYDBQL Candidate`: the editable query editor.
- `Validation / Approval`: validation status and accepted query state.
- `Execution Preview`: the executed HTTP command, response path, row count, and response summary.

## Keyboard Shortcuts

| Shortcut | Action |
| --- | --- |
| `Tab` | Move focus to the next input or editor. |
| `Shift+Tab` | Move focus to the previous input or editor. |
| `Ctrl+R` | Cycle resource type: `MEASURE`, `STREAM`, `TRACE`, `PROPERTY`, `TOPN`. |
| `Ctrl+A` | Ask the configured agent to generate or revise BYDBQL. |
| `Ctrl+V` | Validate the current BYDBQL editor content. |
| `Ctrl+E` | Execute the current valid BYDBQL query through the workflow-owned query endpoint. |
| `Ctrl+X` | Accept the current executed BYDBQL as the final query. |
| `Esc` or `Ctrl+C` | Quit. |

## Generate BYDBQL with the Agent

1. Start `bydbctl agent`.
2. Use `Tab` to focus `Goal`.
3. Enter a natural language request, for example:

```text
Show the top 10 slow endpoints of payment-service in the last 30 minutes.
```

4. Fill the slots:

```text
Type: MEASURE
Name: service_endpoint_latency
Groups: sw_metrics
Start: -30m
End:
```

5. Press `Ctrl+A`.

The TUI sends the goal, slots, schema summary (including indexed fields for ORDER BY), time range, query hints, a template baseline query, current BYDBQL candidate, validation errors, and execution errors to the configured agent. The agent returns a BYDBQL candidate. The candidate is written into the `BYDBQL Candidate` editor.

With `--addr` set, bydbctl discovers groups, resource catalogs, schema details, and index rules directly from BanyanDB HTTP APIs before each workflow action. If you only provide a goal, bydbctl auto-matches the best resource from `schema.catalog` and fills the Slots for you. Explicit Name and Groups values pin the slots and override catalog matching. MCP is optional and not required for standalone use.

Goal-only example:

```shell
bydbctl agent \
  --agent acp \
  --acp-command npx \
  --acp-arg -y \
  --acp-arg @agentclientprotocol/claude-agent-acp \
  --addr http://localhost:17913 \
  --goal "top 10 slow payment endpoints in last 30 minutes"
```

bydbctl lists groups from `/api/v1/group/schema/lists`, lists resources per group, scores catalog entries against the goal, and sends the matched schema plus full catalog to the agent.

## Edit, Validate, Execute, and Accept

After the agent generates a query:

1. Edit the `BYDBQL Candidate` text directly if needed.
2. Press `Ctrl+V` to validate the current editor content.
3. Press `Ctrl+E` to execute it after validation passes.
4. Check `Execution Preview` for:
   - command: `POST /api/v1/bydbql/query`
   - response path
   - row count
   - response summary
5. Press `Ctrl+X` to accept the final BYDBQL.

`Ctrl+X` only accepts the current candidate after it has been executed. If you edit the query after execution, execute it again before accepting.

## Revise Again After Execution

You can continue after execution:

1. Edit the BYDBQL manually, or leave the executed query as-is.
2. Press `Ctrl+A`.

The agent receives the current BYDBQL, validation errors, execution errors, and zero-row hints from the last `Ctrl+E` run. This lets it revise the query based on syntax, semantic checks (such as unknown tags/fields or non-indexed ORDER BY fields), or BanyanDB execution failures.

## Safety Rules

- The agent only proposes BYDBQL.
- The BYDBQL editor is always editable before execution.
- The agent cannot accept a final query.
- The agent cannot execute BanyanDB queries directly.
- `bydbctl` validates and executes BYDBQL only after the user requests it with `Ctrl+E`.
- ACP permission requests are denied by the bydbctl workflow by default.

## Common Workflows

Generate from natural language:

```text
Fill Goal and Slots -> Ctrl+A -> edit query -> Ctrl+V -> Ctrl+E -> Ctrl+X
```

Repair an invalid query:

```text
Edit query -> Ctrl+V -> Ctrl+A -> Ctrl+V
```

Revise after seeing results:

```text
Ctrl+E -> review Execution Preview -> Ctrl+A -> Ctrl+V -> Ctrl+E
```

## Troubleshooting

If `Ctrl+A` does not produce a useful query, check these fields first:

- `Name` must be the BanyanDB resource name.
- `Groups` must include the resource group.
- `Type` must match the resource type.
- `Start` should be set for measure, stream, trace, and Top-N queries.

`error: agent returned no BYDBQL candidate` means the agent finished a turn, but `bydbctl` could not find a usable BYDBQL statement in the
response. The workflow expects exactly one `SELECT` or `SHOW TOP` statement, preferably in a fenced `bydbql` code block. Check the session log
file shown in `Events` for the full raw agent output.

`error: agent candidate failed validation` means `bydbctl` did extract a candidate, but the BYDBQL parser rejected it after the configured retry
limit. In this case:

- `BYDBQL Candidate` shows the last invalid query so you can edit it directly.
- `Validation / Approval` shows the parser or transformer error message.
- `Events` shows a short validation hint; the session log contains the full validation message and invalid candidate query.

After fixing the editor content, press `Ctrl+V` to validate again. You can also press `Ctrl+A` again; the next agent request includes the current
candidate and the validation error as repair context.

If `--agent codex-acp` fails to start, verify that `npx` is installed and can download `@agentclientprotocol/codex-acp`.

If Codex ACP starts but cannot use BanyanDB tools, verify `--mcp-config`, `mcp/dist/index.js`, `mcp/tools/bin/bydbql-parse`, and the `BANYANDB_ADDRESS`
inside `.mcp.json`.

If execution fails, check `--addr`, `--username`, and `--password`, then open the session log from `Events` for the HTTP status, error summary, and full agent transcript.
