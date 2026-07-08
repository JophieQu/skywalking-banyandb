# BydbQL Syntax (condensed)

## Core

- Forms: `SELECT ... FROM ...` or `SHOW TOP N FROM MEASURE ...`
- Resource types: `STREAM`, `MEASURE`, `TRACE`, `PROPERTY`
- `FROM <TYPE> <name> IN <group>[, <group>...]` is required
- `TIME` is required for `STREAM`, `MEASURE`, `TRACE`, and `SHOW TOP`; `PROPERTY` does not use `TIME`
- Clause order for SELECT: `SELECT` -> `FROM` -> `TIME` -> `WHERE` -> `GROUP BY` -> `ORDER BY` -> `LIMIT`
- Clause order for SHOW TOP: `SHOW TOP` -> `FROM` -> `TIME` -> `WHERE` -> `AGGREGATE BY` -> `ORDER BY`

## TIME vs LIMIT

- "last 3 days", "past hour" -> `TIME > '-3d'`, `TIME > '-1h'` (time range, no LIMIT)
- "last 30 spans", "top 10 items" -> `ORDER BY <indexed_field> DESC LIMIT 30` (data points)
- When a number appears with a time unit (minutes, hours, days) -> TIME only
- When a number appears before/after a resource name as a count -> LIMIT (usually needs ORDER BY)

## TOPN vs SELECT

- Use `SHOW TOP N FROM MEASURE ...` when goal has ranking words (top, highest, lowest) + number over a measure
- `SHOW TOP` uses `ORDER BY DESC` or `ORDER BY ASC` without a field name
- Do not use LIMIT in SHOW TOP queries

## ORDER BY

- Only use field names from `indexed_fields` in the context JSON
- If the requested sort field is not indexed, substitute the closest indexed field or omit ORDER BY
- TOPN queries: `ORDER BY DESC` or `ORDER BY ASC` only (no field name)
