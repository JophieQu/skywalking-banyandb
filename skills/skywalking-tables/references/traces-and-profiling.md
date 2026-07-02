# SkyWalking Traces and Profiling Storage

Use this reference to understand SkyWalking trace and profiling storage resources in BanyanDB.

## Trace Storage

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

## Profiling Storage

Profiling tasks, segments, schedules, async-profiler data, pprof data, continuous profiling policy, and eBPF data are spread across records, metadata, and properties. Use live schema discovery before raw inspection because the storage resources depend on enabled profiling features and source version.

## Source Catalog

`generated-storage-catalog.md` lists:

- `TRACE segment` in `sw_trace` from `SegmentRecord`.
- Zipkin trace resources in `sw_zipkinTrace`.
- Profiling records in `sw_records`, including trace profile tasks, async-profiler, pprof, eBPF task/data records, and sampled trace records.
- `ebpf_profiling_schedule` in `sw_metadata` and `continuous_profiling_policy` in `sw_property`.
