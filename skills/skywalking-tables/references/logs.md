# SkyWalking Logs and Records in BanyanDB

Use this reference before generating raw BydbQL for SkyWalking logs or record streams. Confirm exact stream names and fields with `list_groups_schemas`.

For source-derived record stream names and model fields, search `generated-storage-catalog.md`.

## Group and Stream Pattern

Common log storage:

- group: `sw_recordsLog`
- stream: `log`

Common fields or tags may include:

- `timestamp`
- `service_id`
- `service_instance_id`
- `endpoint_id`
- `trace_id`
- `content`
- log tags or attributes defined by OAP ingestion

## Raw Query Templates

Recent logs:

```sql
SELECT timestamp, service_id, service_instance_id, endpoint_id, trace_id, content
FROM STREAM log IN sw_recordsLog
TIME > '-15m'
ORDER BY TIME DESC
LIMIT 20
```

Logs for a trace:

```sql
SELECT timestamp, service_id, service_instance_id, endpoint_id, trace_id, content
FROM STREAM log IN sw_recordsLog
TIME > '-1h'
WHERE trace_id = '<trace-id>'
ORDER BY TIME DESC
LIMIT 50
```

Content search, when the content field supports text indexing:

```sql
SELECT timestamp, service_id, trace_id, content
FROM STREAM log IN sw_recordsLog
TIME > '-1h'
WHERE content MATCH('error')
ORDER BY TIME DESC
LIMIT 50
```

If a projected column is rejected, retry with `SELECT *` over a small time range and inspect the actual tag/field names.

## Source Catalog

`generated-storage-catalog.md` derives `sw_recordsLog` / `log` from `LogRecord` and lists storage fields such as `service_id`,
`service_instance_id`, `endpoint_id`, and `trace_id`. It also lists related streams in `sw_records`, including alarm, profiling,
TopN, and sampled trace records.

Use `entity-ids.md` when raw record rows contain encoded SkyWalking IDs that need to be turned back into OAP names for `swctl`.
