# swctl Query Reference

Read this file before constructing or executing `swctl` commands. These patterns are distilled from Apache SkyWalking GitHub docs, `test/e2e-v2` cases, and local `swctl --help`.

## Base Pattern

Use OAP GraphQL as `--base-url`. Use OAP admin as `--admin-url` for all `admin` subcommands.

```bash
swctl --display json --base-url=http://localhost:12800/graphql <command>
swctl --display yaml --base-url=http://localhost:12800/graphql <command>
swctl --display json --admin-url=http://localhost:17128 admin <command>
```

Prefer `--display json` when the answer needs parsing, counting, or summarizing.
Prefer `--display yaml` for direct human inspection.
If the environment uses a different OAP host or port, replace `localhost:12800`; admin commands commonly use port `17128`.

Before execution, check that `swctl` is on `PATH` and OAP GraphQL is reachable:

```bash
swctl --version
swctl --display json --base-url=http://localhost:12800/graphql service ls
```

If `swctl` is missing in a SkyWalking checkout, the e2e setup installs it with `test/e2e-v2/script/prepare/setup-e2e-shell/install.sh swctl`.

## Time Ranges

For any non-default time window, pass both `--start` and `--end`.

```bash
# Default: last 30 minutes.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls

# Last 7 days to now.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --start="-168h" --end="0m"

# Last 1 hour to now.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --start="-1h" --end="0m"

# Absolute minute precision.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --start="2021-10-26 1047" --end="2021-10-26 1127"
```

Important rules:

- Do not use `--end="now"`; use `--end="0m"` for now.
- Do not pass only `--start` for "since start until now". If only `--start` is present, `swctl` calculates `end = start + 30 units` using the start precision.
- Do not pass only `--end` for a custom range. If only `--end` is present, `swctl` calculates `start = end - 30 units` using the end precision.
- If both are absent, the range is the last 30 minutes.
- Relative examples include `-20m`, `-1h`, `-168h`, and `0m`.
- Absolute examples include `2023-01-08`, `2023-01-08 00`, and `2023-01-08 0000`.
- For metrics over long windows, add `--step=HOUR` or `--step=DAY` when appropriate.
- For cold-stage BanyanDB queries, add `--cold=true` or `--cold` when the command supports it.

## Entity Discovery

Use these before trace/profiling commands when service, instance, endpoint, process, or dependency names are unknown.

```bash
swctl --display json --base-url=http://localhost:12800/graphql layer ls
swctl --display json --base-url=http://localhost:12800/graphql service ls
swctl --display json --base-url=http://localhost:12800/graphql service layer GENERAL
swctl --display json --base-url=http://localhost:12800/graphql instance list --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql endpoint list --keyword=<keyword> --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql dependency service --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql dependency instance --service-name=<service-name> --dest-service-name=<dest-service-name>
swctl --display json --base-url=http://localhost:12800/graphql dependency endpoint --service-name=<service-name> --endpoint-name=<endpoint-name>
swctl --display json --base-url=http://localhost:12800/graphql process ls --service-name=<service-name> --instance-name=<instance-name>
swctl --display json --base-url=http://localhost:12800/graphql process estimate scale --service-name=<service-name> --labels <label-selector>
```

`list` and `ls` are both common in e2e cases; prefer the full command in generated instructions unless the command help only documents `ls`.

## MCP-Assisted Entity Discovery

Use BanyanDB MCP as supporting discovery when `swctl` cannot resolve an entity name, for example `no such service`,
or when the user gives an ambiguous label such as "service 1".
When using MCP for this, apply the `skywalking-tables` and `bydbql` skills for storage names, schema context, and raw ID decoding.

Workflow:

1. Try OAP discovery first with `service ls`, then `instance list`, `endpoint list`, or `process ls` when the service is known.
2. If OAP discovery rejects the name or the user provided an unclear label, use MCP `list_groups_schemas` to inspect relevant BanyanDB resources.
3. Prefer read-only metrics or logs for entity discovery:
   - `sw_metricsMinute`, `sw_metricsHour`, or `sw_metricsDay` measures such as `service_cpm_*`, `service_resp_time_*`, and `service_sla_*`.
   - `sw_recordsLog` stream `log` for `service_id`, `service_instance_id`, `endpoint_id`, `trace_id`, and `content`.
   - `sw_trace` trace `segment` for raw trace storage checks.
4. Validate each BydbQL statement before execution. Generate one read-only `SELECT` statement with no semicolon.
5. Decode SkyWalking entity IDs before retrying `swctl`:
   - Service IDs usually look like `<base64(service-name)>.<normal-flag>`, for example `c29uZ3M=.1` means service `songs`.
   - Instance IDs often look like `<service-id>_<base64(instance-name)>`.
   - Endpoint IDs often look like `<service-id>_<base64(endpoint-name)>`.
   - Retry `swctl` with the decoded `--service-name`, `--instance-name`, or `--endpoint-name`.
     Use `--service-id`, `--instance-id`, or `--endpoint-id` when the encoded ID is the only exact match.
6. If MCP finds no matching service or entity candidate, report both the `swctl` rejection and the MCP discovery result instead of inventing a service name.

Useful MCP discovery queries:

```sql
SELECT *
FROM MEASURE service_cpm_minute IN sw_metricsMinute
TIME > '-24h'
ORDER BY TIME DESC
LIMIT 50
```

```sql
SELECT timestamp, service_id, service_instance_id, endpoint_id, trace_id, content
FROM STREAM log IN sw_recordsLog
TIME > '-24h'
ORDER BY TIME DESC
LIMIT 50
```

If a projected metric column such as `service_id` is rejected as not found, query `SELECT *` and parse `entity_id` from the returned tag families.

## Trace Queries

Use trace v2 (`tv2 ls`) for BanyanDB-backed SkyWalking traces.

```bash
# Recent traces, default last 30 minutes.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls

# Recent traces over an explicit range, for example last week.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --start="-168h" --end="0m"

# Filter by service.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --service-name=<service-name> --start="-1h" --end="0m"

# Filter by service and endpoint.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --service-name=<service-name> --endpoint-name=<endpoint-name> --start="-1h" --end="0m"

# Filter by tags.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --tags http.method=POST,http.status_code=201 --start="-1h" --end="0m"

# Fetch by trace ID.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls --trace-id=<trace-id> --start="-1h" --end="0m"

# Sort by start time instead of duration.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls \
  --order startTime \
  --service-name=<service-name> \
  --endpoint-name=<endpoint-name> \
  --start="-1h" \
  --end="0m"

# Query cold-stage storage.
swctl --display json --base-url=http://localhost:12800/graphql tv2 ls \
  --service-name=<service-name> \
  --endpoint-name=<endpoint-name> \
  --start="-96h" \
  --end="-48h" \
  --cold=true
```

Use legacy `trace ls` only when an older SkyWalking e2e case, plugin, or output shape requires it:

```bash
swctl --display json --base-url=http://localhost:12800/graphql trace ls \
  --service-name=<service-name> \
  --endpoint-name=<endpoint-name> \
  --tags http.method=POST,http.status_code=201
```

## Trace Profiling

Use this workflow for `profiling trace` tasks.

```bash
# Create a trace profiling task.
swctl --display json --base-url=http://localhost:12800/graphql profiling trace create \
  --service-name=<service-name> \
  --endpoint-name=<endpoint-name> \
  --start-time=-1 \
  --duration=1 \
  --min-duration-threshold=1500 \
  --dump-period=500 \
  --max-sampling-count=5

# List trace profiling tasks.
swctl --display json --base-url=http://localhost:12800/graphql profiling trace list \
  --service-name=<service-name> \
  --endpoint-name=<endpoint-name>

# List profiled segments for a task.
swctl --display json --base-url=http://localhost:12800/graphql profiling trace segment-list --task-id=<task-id>

# Analyze a profiled segment. Use start/end from the selected segment span.
swctl --display json --base-url=http://localhost:12800/graphql profiling trace analysis \
  --segment-ids=<segment-id> \
  --time-ranges=<start-time>-<end-time>
```

If the task ID or segment ID is unknown, derive them in order:

1. Run `profiling trace list --service-name=<service-name> --endpoint-name=<endpoint-name>` and read the task `id`.
2. Run `profiling trace segment-list --task-id=<task-id>` and choose the relevant span, often the root span where `spanid` is `0`.
3. Read that span's `segmentid`, `starttime`, and `endtime`.
4. Run `profiling trace analysis --segment-ids=<segmentid> --time-ranges=<starttime>-<endtime>`.

## Async Profiler

Use this workflow for JVM async-profiler tasks.

```bash
swctl --display json --base-url=http://localhost:12800/graphql profiling async create \
  --service-name=<service-name> \
  --duration=20 \
  --events=cpu,alloc \
  --instance-name-list=<instance-name>

swctl --display json --base-url=http://localhost:12800/graphql profiling async list --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql profiling async progress --task-id=<task-id>

swctl --display json --base-url=http://localhost:12800/graphql profiling async analysis \
  --service-name=<service-name> \
  --event=execution_sample \
  --instance-name-list=<instance-name> \
  --task-id=<task-id>
```

In e2e output, async task IDs are under `tasks[0].id`.

## pprof Profiling

Use this workflow for Go pprof tasks.

```bash
swctl --display json --base-url=http://localhost:12800/graphql profiling pprof create \
  --service-name=<service-name> \
  --dump-period=1 \
  --events=HEAP \
  --instance-name-list=<instance-name>

swctl --display json --base-url=http://localhost:12800/graphql profiling pprof list --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql profiling pprof progress --task-id=<task-id>

swctl --display json --base-url=http://localhost:12800/graphql profiling pprof analysis \
  --service-name=<service-name> \
  --instance-name-list=<instance-name> \
  --task-id=<task-id>
```

In e2e output, pprof task IDs are under `tasks[0].id`.

## eBPF Profiling

Use this workflow for Rover/eBPF profiling. It usually requires Kubernetes/Rover process metadata.

```bash
# Discover process metadata.
swctl --display json --base-url=http://localhost:12800/graphql process ls --service-name=<service-name> --instance-name=<instance-name>
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf create prepare --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql process estimate scale --service-name=<service-name> --labels <label-selector>

# Create an on-CPU fixed task.
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf create fixed \
  --service-name=<service-name> \
  --labels <label-selector> \
  --duration 1m

# Create an off-CPU fixed task.
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf create fixed \
  --service-name=<service-name> \
  --labels <label-selector> \
  --duration 1m \
  --target-type OFF_CPU

# List tasks and schedules.
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf list --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf schedules --task-id=<task-id>

# Analyze a schedule. Use start/end from the schedule.
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf analysis \
  --schedule-id=<schedule-id> \
  --time-ranges=<start-time>-<end-time>

# Optional aggregation used by off-CPU e2e cases.
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf analysis \
  --schedule-id=<schedule-id> \
  --time-ranges=<start-time>-<end-time> \
  --aggregate=COUNT

swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf analysis \
  --schedule-id=<schedule-id> \
  --time-ranges=<start-time>-<end-time> \
  --aggregate=DURATION
```

In e2e output, eBPF task IDs are commonly under `[0].taskid`, schedule IDs under `[0].scheduleid`, and schedule windows under `[0].starttime` / `[0].endtime`.

Continuous profiling e2e patterns:

```bash
swctl --display json --base-url=http://localhost:12800/graphql profiling continuous set --service-name=<service-name> --config <policy.yaml>
swctl --display json --base-url=http://localhost:12800/graphql profiling continuous ls --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql profiling continuous monitoring --service-name=<service-name> --target On_CPU
swctl --display json --base-url=http://localhost:12800/graphql profiling ebpf list --service-name=<service-name> --trigger CONTINUOUS_PROFILING
swctl --display json --base-url=http://localhost:12800/graphql metrics exec \
  --service-name=<service-name> \
  --instance-name=<instance-name> \
  --process-name=<process-name> \
  --expression=continuous_profiling_process_cpu
```

## Logs and Metrics

```bash
# Logs, optionally constrained by tags and trace ID.
swctl --display json --base-url=http://localhost:12800/graphql logs list --service-name=<service-name> --tags level=INFO --trace-id=<trace-id>

# Single-value metrics and nullable metrics.
swctl --display json --base-url=http://localhost:12800/graphql metrics single --name=service_sla --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql metrics nullable --name=service_sla --service-name=<service-name>

# Metric expressions by service, instance, endpoint, or process.
swctl --display json --base-url=http://localhost:12800/graphql metrics exec --expression=service_cpm --service-name=<service-name>
swctl --display json --base-url=http://localhost:12800/graphql metrics exec \
  --expression=service_instance_cpm \
  --service-name=<service-name> \
  --instance-name=<instance-name>
swctl --display json --base-url=http://localhost:12800/graphql metrics exec --expression=endpoint_resp_time --service-name=<service-name> --endpoint-name=<endpoint-name>
swctl --display json --base-url=http://localhost:12800/graphql metrics exec \
  --expression=continuous_profiling_process_cpu \
  --service-name=<service-name> \
  --instance-name=<instance-name> \
  --process-name=<process-name>

# TopN and MQE examples.
swctl --display json --base-url=http://localhost:12800/graphql metrics top --name service_sla 5
swctl --display json --base-url=http://localhost:12800/graphql metrics sorted --name service_apdex 5
swctl --display json --base-url=http://localhost:12800/graphql metrics exec --expression="top_n(service_resp_time,3,des)"
swctl --display json --base-url=http://localhost:12800/graphql metrics exec --expression="top_n(service_resp_time,3,des,attr0='GENERAL')"
```

Admin e2e patterns use `--admin-url` instead of `--base-url`:

```bash
swctl --display json --admin-url=http://localhost:17128 admin inspect metrics --regex service_cpm
swctl --display json --admin-url=http://localhost:17128 admin inspect entities --metric service_cpm --start "2023-01-08" --end "2023-01-08" --step DAY
swctl --display json --admin-url=http://localhost:17128 admin inspect entities --metric service_cpm --start "-30m" --end "0m" --step MINUTE
```

## Parsing and Execution Guidance

If command output must be inspected programmatically, prefer `--display json`.
Use available local tools to parse JSON; do not assume `jq` or `yq` is installed.
The SkyWalking e2e examples often use `yq`, but a Codex task can also parse JSON with Python, Node, or the host language.

If execution is requested and all inputs are known, run the exact command.
If inputs are missing, run discovery commands first when safe; otherwise return the concrete command template and list the missing inputs.
If `swctl` or OAP is unavailable, do not fabricate results; return the command and the blocker.
