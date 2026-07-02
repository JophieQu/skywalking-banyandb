# SkyWalking Storage Families

This file summarizes SkyWalking data families in BanyanDB.
Confirm exact groups, resources, tags, and fields with live schema discovery before executing BydbQL.
For source-derived names and owning files, read `generated-storage-catalog.md`, `generated-metrics-catalog.md`, and `generated-topn.md`.

## Families

Metrics:

- BanyanDB type: `MEASURE`.
- Common groups: `sw_metricsMinute`, `sw_metricsHour`, `sw_metricsDay`.
- Common resources: `service_cpm_*`, `service_resp_time_*`, `service_sla_*`, endpoint/instance/process metrics.
- Source catalog: `generated-metrics-catalog.md`.

Logs and records:

- BanyanDB type: `STREAM`.
- Common groups: `sw_recordsLog`.
- Common resources: `log`.
- Source catalog: `generated-storage-catalog.md`.

Traces:

- BanyanDB type: `TRACE`.
- Common groups: `sw_trace`.
- Common resources: commonly `segment`; confirm live schema.
- Source catalog: `generated-storage-catalog.md`.

Properties:

- BanyanDB type: `PROPERTY`.
- Common groups: `sw_property` for current source-derived property resources.
- Common resources: examples include `continuous_profiling_policy`, `runtimerule`, and `ui_template`.
- Source catalog: `generated-storage-catalog.md`.

Profiling:

- BanyanDB type: mixed OAP-managed records.
- Common groups: `sw_records`, `sw_metadata`, and `sw_property`.
- Common resources: task, schedule, log, and uploaded data records for trace, async-profiler, pprof, continuous profiling, and eBPF.
- Source catalog: `generated-storage-catalog.md`.

## Granularity

SkyWalking metrics are commonly split by time granularity:

- minute: group names often end in `Minute`, resource names often end in `_minute`.
- hour: group names often end in `Hour`, resource names often end in `_hour`.
- day: group names often end in `Day`, resource names often end in `_day`.

Do not infer a resource exists only from this pattern. Use `list_groups_schemas` when the exact group or measure name is not already known.
