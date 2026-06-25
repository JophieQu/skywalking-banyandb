# Query Routing: BydbQL vs swctl

Classify every SkyWalking observability query before choosing a tool.

## Default Rule

Route by resource family first, not by whether the user wants a raw or UI-like answer.

- Logs, metrics, and properties use BydbQL.
- Traces and profiling use swctl, or another SkyWalking OAP GraphQL client.

Do not query live BanyanDB with MCP tools when the user only asks for guidance. Explain the route.

## Logs

BanyanDB storage:

- Usually stored as `Stream` records, commonly under groups such as `sw_recordsLog`.
- Log fields such as `timestamp`, `service_id`, `service_instance_id`, `endpoint_id`, `trace_id`, `content`, and tags are generally queryable as raw stream tags.

Use BydbQL for log queries by default, including raw ingestion checks, stored row inspection, tag checks, and raw content search.

Example:

```sql
SELECT timestamp, service_id, service_instance_id, endpoint_id, trace_id, content
FROM STREAM log IN sw_recordsLog
TIME > '-15m'
LIMIT 20
```

Only switch to swctl when the user explicitly asks for OAP GraphQL or a `swctl` command.

## Metrics

BanyanDB storage:

- SkyWalking metrics are primarily `Measure` resources.
- Common groups are `sw_metricsMinute`, `sw_metricsHour`, and `sw_metricsDay`.
- Tags hold dimensions such as entity/service/instance/endpoint IDs. Fields hold metric values such as `value`, `total`, or counts.
- Some metadata-like metrics use BanyanDB `index_mode`; TopN is often precomputed into opaque/internal result measures.

Use BydbQL for metric queries by default, including raw measure points, tags, fields, groups, schema, entity IDs, and data-existence checks.

Example:

```sql
SELECT entity_id, service_id, value
FROM MEASURE service_cpm_minute IN sw_metricsMinute
TIME > '-30m'
LIMIT 20
```

Only switch to swctl when the user explicitly asks for OAP GraphQL or a `swctl` command.

## Traces

Use swctl for trace queries. Do not synthesize BydbQL trace queries in this skill.

Use MCP only as auxiliary discovery when OAP entity names are missing, ambiguous, or rejected by `swctl` errors such as `no such service`. In that case, discover BanyanDB groups/schemas and query read-only metrics or logs to infer service, instance, endpoint, or trace IDs, then retry the final trace query with `swctl`. Do not query `TRACE` resources with BydbQL as a substitute for trace results.

Typical trace requests:

- list recent traces
- filter traces by service, endpoint, or tags
- inspect trace trees and spans
- retrieve UI-equivalent trace results from OAP

See `references/swctl.md` for concrete commands built from repo e2e cases.

## Profiling

Use swctl for profiling trace workflows. Do not synthesize BydbQL profiling queries in this skill.

Typical profiling requests:

- create a profiling task
- list profiling tasks
- list profiled trace segments for a task
- analyze a segment over a time range

See `references/swctl.md` for concrete commands built from repo e2e cases.

## Properties

Use BydbQL for BanyanDB properties and metadata-style key/value records.

## Decision Checklist

- Mentions logs, log rows, log tags, or log content: BydbQL.
- Mentions metrics, measures, metric values, or metric TopN in BanyanDB: BydbQL.
- Mentions trace, span, trace tree, `tv2`, or trace list: swctl.
- Mentions profiling, flame graph, task, segment analysis, async-profiler, pprof, or eBPF profiling: swctl.
- Mentions property or metadata key/value records: BydbQL.
- If the user explicitly asks for `swctl`, provide `swctl` even for logs or metrics.
