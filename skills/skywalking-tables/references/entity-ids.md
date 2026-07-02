# SkyWalking Entity IDs

Generated from `oap-server/server-core/src/main/java/org/apache/skywalking/oap/server/core/analysis/IDManager.java` in `/home/ququ/Programs/skywalking` at `2026-06-28T14:02:57+00:00`.

Use this reference when raw BanyanDB rows contain SkyWalking storage IDs but `swctl` needs OAP names.

## Encoding Rules

- String parts are Base64-encoded UTF-8.
- Service ID: `<base64(service-name)>.<normal-flag>`.
- Service ID with layer: `<service-id>.<layer-value>`.
- Instance ID: `<service-id>_<base64(instance-name)>`.
- Instance ID with layer: `<instance-id>.<layer-value>`.
- Endpoint ID: `<service-id>_<base64(endpoint-name)>`.
- Service relation ID: `<base64(source-service-id)>_<detect-point>_<base64(dest-service-id)>`.
- Process ID: `<instance-id>_<base64(process-name)>`.
- Network address alias ID: `<base64(address)>`.
- Service label ID: `<service-id>_<base64(label)>`.

## Usage

- For `swctl`, prefer decoded OAP names such as `--service-name`, `--instance-name`, `--endpoint-name`, and `--process-name`.
- Use raw ID flags only when OAP discovery shows the encoded ID is the only exact match.
- For BydbQL, raw rows commonly filter by `entity_id`, `service_id`, `service_instance_id`, `endpoint_id`, `process_id`, or relation IDs.

## Discovery Workflow

1. Run OAP discovery first with `swctl service ls`, `instance list`, `endpoint list`, or `process list`.
2. If OAP rejects a name, inspect recent metrics/log rows with BydbQL to find raw IDs.
3. Decode obvious Base64 components and retry `swctl` with decoded names.
4. If decoding is ambiguous, report the raw ID and attempted decoded name.
