# SkyWalking Metrics in BanyanDB

Use this reference before generating raw BydbQL for SkyWalking metrics. Confirm exact names with `list_groups_schemas`.

For complete source-derived metric names, search `generated-metrics-catalog.md`. For TopN rules, read `generated-topn.md`.

## Group Pattern

Common metric groups:

- `sw_metricsMinute`
- `sw_metricsHour`
- `sw_metricsDay`

Metric resource names often combine the OAP metric expression and the storage granularity, for example:

- `service_cpm_minute`, `service_cpm_hour`, `service_cpm_day`
- `service_resp_time_minute`, `service_resp_time_hour`, `service_resp_time_day`
- `service_sla_minute`, `service_sla_hour`, `service_sla_day`

This is a naming heuristic, not a guarantee.

## Tags and Fields

Common dimensions may include:

- `entity_id`
- `service_id`
- `service_instance_id`
- `endpoint_id`
- attribute tags such as `attr0`, `attr1`, or OAP-defined dimensions

Common numeric fields may include:

- `value`
- `total`
- count or bucket fields for specific metric types

When a field is uncertain, start with `SELECT *` over a narrow time range, inspect the returned tag families and fields, then refine the projection.

## Raw Query Templates

Recent raw metric points:

```sql
SELECT *
FROM MEASURE service_cpm_minute IN sw_metricsMinute
TIME > '-30m'
LIMIT 20
```

Recent metric points for one entity:

```sql
SELECT *
FROM MEASURE service_cpm_minute IN sw_metricsMinute
TIME > '-30m'
WHERE entity_id = '<entity-id>'
LIMIT 20
```

Top or bottom metric ranking:

```sql
SHOW TOP 10
FROM MEASURE service_cpm_minute IN sw_metricsMinute
TIME > '-1h'
AGGREGATE BY SUM
ORDER BY DESC
```

## Source Catalogs

- `generated-metrics-catalog.md` maps OAL/MAL metric expressions to expected BanyanDB measure resources and source files.
- `generated-topn.md` maps `bydb-topn.yml` rules to their metric names, grouping tags, and sort direction.
- `generated-storage-catalog.md` includes model-derived metadata measures such as `service_traffic`, `instance_traffic`, and relation metrics.

Use these catalogs to select candidate names, then confirm the live group/resource with `list_groups_schemas` before execution.
