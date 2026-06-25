# Natural Language Examples

These are BydbQL-route examples only. Do not use these examples for trace or profiling requests. For those, see `references/swctl.md`.

```text
"Query service_cpm_minute in metricsMinute for the last 30 minutes"
=> SELECT * FROM MEASURE service_cpm_minute IN metricsMinute TIME > '-30m'
```

```text
"Find logs where service is frontend"
=> SELECT * FROM STREAM <stream_name> IN <group> WHERE service = 'frontend'
```

```text
"Show the top 10 services by cpm over the last hour"
=> SHOW TOP 10 FROM MEASURE service_cpm_minute IN metricsMinute TIME > '-1h' AGGREGATE BY SUM ORDER BY DESC
```

```text
"Show properties for server metadata in datacenter-1"
=> SELECT * FROM PROPERTY server_metadata IN datacenter-1
```

Use `list_groups_schemas` to replace placeholders such as `<stream_name>` or `<group>` before execution.
