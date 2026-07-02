---
name: skywalking-tables
description: >
  Explain and map SkyWalking storage tables and resources in BanyanDB. Use when
  the agent needs SkyWalking BanyanDB group names, metric/log/property/trace
  storage families, profiling storage records, entity ID conventions, or schema
  assumptions. This skill is a storage-domain catalog, not query execution.
---

# SkyWalking Tables in BanyanDB

Use this skill when a task depends on how SkyWalking data is stored in BanyanDB. It is a domain reference, not a query runner.

It can be combined with:

- `bydbql` for raw BanyanDB query generation, validation, and execution.
- `swctl-query` for OAP GraphQL command construction when storage context or ID decoding is useful.

## Reference Map

- Storage family overview from SkyWalking source: read `references/generated-storage-catalog.md`.
- Metric lookup from SkyWalking OAL/MAL source: read `references/generated-metrics-catalog.md`.
- TopN rules from SkyWalking source: read `references/generated-topn.md`.
- Metrics query templates: read `references/metrics.md`.
- Logs and record query templates: read `references/logs.md`.
- Entity IDs and OAP name decoding: read `references/entity-ids.md`.
- Trace and profiling storage resources: read `references/traces-and-profiling.md`.
- Storage family summary: read `references/storage-families.md`.

## Workflow

1. Classify the SkyWalking data family: metrics, logs/records, traces, profiling, properties, or entities.
2. Read only the matching reference files. Prefer generated catalogs for exact source-derived names, and hand-written references for query templates and storage notes.
3. Treat these references as schema guidance. Use live schema discovery through `list_groups_schemas` before executing BydbQL when names are missing or uncertain.
4. Prefer SkyWalking/OAP names for `swctl` commands. Decode storage entity IDs only when needed to recover OAP names or explain raw rows.
5. When generated and live schema disagree, trust live BanyanDB for execution and cite the generated source catalog as the static expectation.

## Refreshing References

Regenerate catalogs from the preferred local SkyWalking checkout with:

```bash
python3 skills/skywalking-tables/scripts/extract_skywalking_storage.py --skywalking-root /home/ququ/Programs/skywalking
```

When the local checkout is unavailable, use the GitHub fallback:

```bash
python3 skills/skywalking-tables/scripts/extract_skywalking_storage.py --github-repo apache/skywalking --github-ref master
```
