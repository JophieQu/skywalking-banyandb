# SkyWalking Traces and Profiling Storage

Use this reference to decide whether a request should inspect raw BanyanDB trace storage or reuse SkyWalking OAP GraphQL through `swctl`.

## Trace Boundary

Use `swctl-query` for:

- trace lists.
- trace tree or span details.
- trace ID lookup intended to match the SkyWalking UI.
- filters by service, endpoint, tags, duration, or status when the expected result is an OAP trace view.

Use `bydbql` only when the user explicitly asks for raw BanyanDB trace storage, trace schema inspection, or storage existence checks.

## Raw Trace Storage Template

Common trace storage:

- group: `sw_trace`
- trace resource: `segment` in the generated SkyWalking source catalog; confirm with `list_groups_schemas`

Template for storage inspection:

```sql
SELECT ()
FROM TRACE segment IN sw_trace
TIME > '-30m'
LIMIT 20
```

If the resource name differs, discover traces in the `sw_trace` group before generating the final query.

## Profiling Boundary

Profiling tasks, segments, schedules, flame graphs, async-profiler, pprof, and eBPF analysis are OAP-managed workflows. Use `swctl-query`.

Do not synthesize raw BydbQL profiling queries unless the user explicitly asks for BanyanDB profiling storage rows and the schema has been discovered.

## Source Catalog

`generated-storage-catalog.md` lists:

- `TRACE segment` in `sw_trace` from `SegmentRecord`.
- Zipkin trace resources in `sw_zipkinTrace`.
- Profiling records in `sw_records`, including trace profile tasks, async-profiler, pprof, eBPF task/data records, and sampled trace records.
- `ebpf_profiling_schedule` in `sw_metadata` and `continuous_profiling_policy` in `sw_property`.

Use these for raw storage inspection only. Use `swctl-query` for UI-equivalent trace and profiling output.
