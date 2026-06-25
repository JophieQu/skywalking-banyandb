#!/usr/bin/env python3
"""Generate SkyWalking BanyanDB storage references from SkyWalking GitHub source."""

from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import re
import shutil
import tempfile
import urllib.request
import zipfile
from dataclasses import dataclass, field
from pathlib import Path


NAMESPACE = "sw"
STREAM_GROUPS = {
    "RECORDS": "records",
    "RECORDS_LOG": "recordsLog",
    "RECORDS_BROWSER_ERROR_LOG": "recordsBrowserErrorLog",
}
TRACE_GROUPS = {
    "TRACE": "trace",
    "ZIPKIN_TRACE": "zipkinTrace",
}
METRIC_GROUPS = {
    "minute": "metricsMinute",
    "hour": "metricsHour",
    "day": "metricsDay",
}
METRIC_RESOURCE_SUFFIXES = {
    "minute": "minute",
    "hour": "hour",
    "day": "day",
}
DEFAULT_GITHUB_REPO = "apache/skywalking"
DEFAULT_GITHUB_REF = "master"


@dataclass
class FieldInfo:
    name: str
    java_name: str
    type_name: str = ""
    measure_field: bool = False
    series_id: int | None = None
    sharding_key: int | None = None
    sortable: bool = False
    analyzer: str | None = None
    no_indexing: bool = False
    storage_only: bool = False
    index_only: bool = False


@dataclass
class ModelInfo:
    class_name: str
    path: Path
    parent: str | None = None
    index_name: str | None = None
    processor: str | None = None
    stream_group: str | None = None
    trace_group: str | None = None
    timestamp_column: str | None = None
    trace_id_column: str | None = None
    span_id_column: str | None = None
    trace_index_rules: list[str] = field(default_factory=list)
    index_mode: bool = False
    constants: dict[str, str] = field(default_factory=dict)
    fields: list[FieldInfo] = field(default_factory=list)


@dataclass
class MetricDefinition:
    name: str
    source: str
    function: str
    path: Path
    line_number: int
    family: str


@dataclass
class TopNRule:
    name: str
    metric_name: str
    group_by: list[str]
    sort: str
    path: Path
    line_number: int


@dataclass(frozen=True)
class SourceRoot:
    path: Path
    label: str


def download_github_archive(repo: str, ref: str, destination: Path) -> None:
    url = f"https://github.com/{repo}/archive/{ref}.zip"
    request = urllib.request.Request(url, headers={"User-Agent": "skywalking-banyandb-skill-generator"})
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=destination.parent, prefix=f"{destination.name}.", suffix=".tmp", delete=False) as temp_file:
        temp_path = Path(temp_file.name)
        with urllib.request.urlopen(request, timeout=120) as response:
            shutil.copyfileobj(response, temp_file)
    temp_path.replace(destination)


def valid_zip(path: Path) -> bool:
    try:
        with zipfile.ZipFile(path) as archive:
            return archive.testzip() is None
    except zipfile.BadZipFile:
        return False


def archive_cache_path(cache_dir: Path, repo: str, ref: str) -> Path:
    safe_repo = repo.replace("/", "-")
    safe_ref = ref.replace("/", "-")
    return cache_dir / f"{safe_repo}-{safe_ref}.zip"


@contextlib.contextmanager
def open_skywalking_source(local_root: Path | None, github_repo: str, github_ref: str, github_cache: Path | None):
    if local_root is not None:
        yield SourceRoot(path=local_root.resolve(), label=str(local_root.resolve()))
        return
    label = f"https://github.com/{github_repo}/tree/{github_ref}"
    with tempfile.TemporaryDirectory(prefix="skywalking-github-") as temp_dir:
        temp_path = Path(temp_dir)
        archive_path = archive_cache_path(github_cache, github_repo, github_ref) if github_cache else temp_path / "skywalking.zip"
        if archive_path.exists() and not valid_zip(archive_path):
            archive_path.unlink()
        if not archive_path.exists():
            download_github_archive(github_repo, github_ref, archive_path)
        extract_dir = temp_path / "source"
        with zipfile.ZipFile(archive_path) as archive:
            archive.extractall(extract_dir)
        roots = [path for path in extract_dir.iterdir() if path.is_dir()]
        if not roots:
            raise RuntimeError(f"downloaded GitHub archive for {github_repo}@{github_ref} did not contain a source directory")
        yield SourceRoot(path=roots[0], label=label)


def group_name(raw_group: str) -> str:
    return f"{NAMESPACE}_{raw_group}" if NAMESPACE else raw_group


def relative(path: Path, root: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)


def collect_java_files(skywalking_root: Path) -> list[Path]:
    roots = [
        skywalking_root / "oap-server" / "server-core" / "src" / "main" / "java",
        skywalking_root / "oap-server" / "server-storage-plugin" / "storage-banyandb-plugin" / "src" / "main" / "java",
    ]
    files: list[Path] = []
    for root in roots:
        if root.exists():
            files.extend(sorted(root.rglob("*.java")))
    return files


def parse_constants(text: str) -> dict[str, str]:
    constants: dict[str, str] = {}
    for match in re.finditer(r"(?:public|protected|private)?\s*static\s+final\s+String\s+([A-Z0-9_]+)\s*=\s*\"([^\"]*)\"", text):
        constants[match.group(1)] = match.group(2)
    return constants


def resolve_ref(ref: str | None, model: ModelInfo, classes: dict[str, ModelInfo]) -> str | None:
    if not ref:
        return None
    ref = ref.strip()
    if ref.startswith('"') and ref.endswith('"'):
        return ref.strip('"')
    if "." in ref:
        class_name, const_name = ref.rsplit(".", 1)
        class_name = class_name.split()[-1]
        target = classes.get(class_name)
        if target and const_name in target.constants:
            return target.constants[const_name]
    if ref in model.constants:
        return model.constants[ref]
    if model.parent and model.parent in classes:
        parent = classes[model.parent]
        if ref in parent.constants:
            return parent.constants[ref]
    return ref


def first_annotation_arg(pattern: str, text: str) -> str | None:
    match = re.search(pattern, text, re.S)
    if not match:
        return None
    for group in match.groups():
        if group:
            return group
    return None


def parse_field_annotations(text: str, model: ModelInfo, classes: dict[str, ModelInfo]) -> list[FieldInfo]:
    fields: list[FieldInfo] = []
    lines = text.splitlines()
    annotations: list[str] = []
    balance = 0
    for line in lines:
        stripped = line.strip()
        if stripped.startswith("@"):
            annotations.append(stripped)
            balance += stripped.count("(") - stripped.count(")")
            continue
        if annotations and balance > 0:
            annotations.append(stripped)
            balance += stripped.count("(") - stripped.count(")")
            continue
        field_match = re.match(r"(?:private|protected|public)\s+(?!static)([\w<>\[\].?, ]+?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:=|;)", stripped)
        if not field_match:
            if stripped and not stripped.startswith("@"):
                annotations = []
                balance = 0
            continue
        annotation_text = "\n".join(annotations)
        field_type = " ".join(field_match.group(1).split())
        java_name = field_match.group(2)
        column_ref = first_annotation_arg(r"@Column\s*\([^)]*name\s*=\s*([A-Za-z0-9_.]+|\"[^\"]+\")", annotation_text)
        field_name = resolve_ref(column_ref, model, classes) or java_name
        series_match = re.search(r"@BanyanDB\.SeriesID\s*\(\s*index\s*=\s*(-?\d+)\s*\)", annotation_text)
        sharding_match = re.search(r"@BanyanDB\.ShardingKey\s*\(\s*index\s*=\s*(-?\d+)\s*\)", annotation_text)
        analyzer_match = re.search(r"@BanyanDB\.MatchQuery\s*\([^)]*AnalyzerType\.([A-Z]+)", annotation_text)
        fields.append(
            FieldInfo(
                name=field_name,
                java_name=java_name,
                type_name=field_type,
                measure_field="@BanyanDB.MeasureField" in annotation_text,
                series_id=int(series_match.group(1)) if series_match else None,
                sharding_key=int(sharding_match.group(1)) if sharding_match else None,
                sortable="@BanyanDB.EnableSort" in annotation_text,
                analyzer=analyzer_match.group(1).lower() if analyzer_match else None,
                no_indexing="@BanyanDB.NoIndexing" in annotation_text,
                storage_only="storageOnly = true" in annotation_text,
                index_only="indexOnly = true" in annotation_text,
            )
        )
        annotations = []
        balance = 0
    return fields


def parse_java_models(skywalking_root: Path) -> dict[str, ModelInfo]:
    classes: dict[str, ModelInfo] = {}
    pending_fields: dict[str, str] = {}
    for path in collect_java_files(skywalking_root):
        text = path.read_text(encoding="utf-8", errors="ignore")
        class_match = re.search(r"\bclass\s+([A-Za-z0-9_]+)(?:\s+extends\s+([A-Za-z0-9_]+))?", text)
        if not class_match:
            continue
        class_name = class_match.group(1)
        model = ModelInfo(
            class_name=class_name,
            parent=class_match.group(2),
            path=path,
            constants=parse_constants(text),
        )
        model.index_name = model.constants.get("INDEX_NAME")
        processor_match = re.search(r"@Stream\s*\([^)]*processor\s*=\s*([A-Za-z0-9_]+)\.class", text, re.S)
        model.processor = processor_match.group(1) if processor_match else None
        stream_group_match = re.search(r"@BanyanDB\.Group\s*\([^)]*streamGroup\s*=\s*BanyanDB\.StreamGroup\.([A-Z_]+)", text, re.S)
        trace_group_match = re.search(r"@BanyanDB\.Group\s*\([^)]*traceGroup\s*=\s*BanyanDB\.TraceGroup\.([A-Z_]+)", text, re.S)
        model.stream_group = stream_group_match.group(1) if stream_group_match else None
        model.trace_group = trace_group_match.group(1) if trace_group_match else None
        model.index_mode = "@BanyanDB.IndexMode" in text
        pending_fields[class_name] = text
        classes[class_name] = model

    for model in classes.values():
        text = pending_fields[model.class_name]
        model.timestamp_column = resolve_ref(
            first_annotation_arg(r"@BanyanDB\.TimestampColumn\s*\(\s*([A-Za-z0-9_.]+|\"[^\"]+\")\s*\)", text),
            model,
            classes,
        )
        model.trace_id_column = resolve_ref(
            first_annotation_arg(r"@BanyanDB\.Trace\.TraceIdColumn\s*\(\s*([A-Za-z0-9_.]+|\"[^\"]+\")\s*\)", text),
            model,
            classes,
        )
        model.span_id_column = resolve_ref(
            first_annotation_arg(r"@BanyanDB\.Trace\.SpanIdColumn\s*\(\s*([A-Za-z0-9_.]+|\"[^\"]+\")\s*\)", text),
            model,
            classes,
        )
        for index_rule in re.finditer(r"@BanyanDB\.Trace\.IndexRule\s*\((.*?)\)", text, re.S):
            rule_name = resolve_ref(first_annotation_arg(r"name\s*=\s*([A-Za-z0-9_.]+|\"[^\"]+\")", index_rule.group(1)), model, classes)
            if rule_name:
                model.trace_index_rules.append(rule_name)
        model.fields = parse_field_annotations(text, model, classes)
    return classes


def inherited_fields(model: ModelInfo, classes: dict[str, ModelInfo], seen: set[str] | None = None) -> list[FieldInfo]:
    seen = seen or set()
    if model.class_name in seen:
        return list(model.fields)
    seen.add(model.class_name)
    fields: list[FieldInfo] = []
    if model.parent and model.parent in classes:
        fields.extend(inherited_fields(classes[model.parent], classes, seen))
    fields.extend(model.fields)
    deduped: dict[str, FieldInfo] = {}
    for field_info in fields:
        deduped[field_info.name] = field_info
    return list(deduped.values())


def model_kind(model: ModelInfo) -> str | None:
    if not model.index_name:
        return None
    if model.trace_group:
        return "TRACE"
    if model.processor == "ManagementStreamProcessor":
        return "PROPERTY"
    if model.index_mode or model.processor == "MetricsStreamProcessor":
        return "MEASURE"
    if model.stream_group or model.processor in {"RecordStreamProcessor", "NoneStreamProcessor", "TopNStreamProcessor"}:
        return "STREAM"
    return None


def storage_rows(classes: dict[str, ModelInfo]) -> list[dict[str, str]]:
    rows: list[dict[str, str]] = []
    for model in sorted(classes.values(), key=lambda item: (item.index_name or "", item.class_name)):
        kind = model_kind(model)
        if not kind or not model.index_name:
            continue
        fields = inherited_fields(model, classes)
        field_names = [field_info.name for field_info in fields]
        measure_fields = [field_info.name for field_info in fields if field_info.measure_field]
        series_ids = [
            f"{field_info.name}:{field_info.series_id}" for field_info in sorted(fields, key=lambda item: item.series_id if item.series_id is not None else 999)
            if field_info.series_id is not None and field_info.series_id >= 0
        ]
        sortable = [field_info.name for field_info in fields if field_info.sortable]
        analyzers = [f"{field_info.name}:{field_info.analyzer}" for field_info in fields if field_info.analyzer]
        if kind == "TRACE":
            group = group_name(TRACE_GROUPS.get(model.trace_group or "", model.trace_group or "trace"))
            resources = [model.index_name]
        elif kind == "STREAM":
            group = group_name(STREAM_GROUPS.get(model.stream_group or "RECORDS", "records"))
            resources = [model.index_name]
        elif kind == "PROPERTY":
            group = group_name("property")
            resources = [model.index_name]
        else:
            if model.index_mode:
                group = group_name("metadata")
                resources = [model.index_name]
            else:
                group = group_name(METRIC_GROUPS["minute"])
                resources = [f"{model.index_name}_{METRIC_RESOURCE_SUFFIXES['minute']}"]
        rows.append(
            {
                "kind": kind,
                "group": group,
                "resources": ", ".join(resources),
                "model": model.class_name,
                "timestamp": model.timestamp_column or "",
                "series": ", ".join(series_ids),
                "fields": ", ".join(measure_fields[:8] or field_names[:8]),
                "sortable": ", ".join(sortable),
                "match": ", ".join(analyzers),
                "source": str(model.path),
            }
        )
    return rows


def parse_oal_metrics(skywalking_root: Path) -> list[MetricDefinition]:
    metrics: list[MetricDefinition] = []
    oal_root = skywalking_root / "oap-server" / "server-starter" / "src" / "main" / "resources" / "oal"
    if not oal_root.exists():
        return metrics
    pattern = re.compile(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*from\(([^)]*)\)\.([A-Za-z0-9_]+)")
    for path in sorted(oal_root.glob("*.oal")):
        for line_number, line in enumerate(path.read_text(encoding="utf-8", errors="ignore").splitlines(), 1):
            match = pattern.match(line)
            if not match:
                continue
            metrics.append(
                MetricDefinition(
                    name=match.group(1),
                    source=match.group(2),
                    function=match.group(3),
                    path=path,
                    line_number=line_number,
                    family="OAL",
                )
            )
    return metrics


def parse_mal_metrics(skywalking_root: Path) -> list[MetricDefinition]:
    metrics: list[MetricDefinition] = []
    mal_root = skywalking_root / "oap-server" / "server-starter" / "src" / "main" / "resources" / "meter-analyzer-config"
    if not mal_root.exists():
        return metrics
    for path in sorted(mal_root.glob("*.yaml")):
        lines = path.read_text(encoding="utf-8", errors="ignore").splitlines()
        for line_number, line in enumerate(lines, 1):
            match = re.match(r"\s*-\s+name:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$", line)
            if not match:
                continue
            metrics.append(
                MetricDefinition(
                    name=match.group(1),
                    source=path.stem,
                    function="meter",
                    path=path,
                    line_number=line_number,
                    family="MAL",
                )
            )
    return metrics


def parse_topn_rules(skywalking_root: Path) -> list[TopNRule]:
    path = skywalking_root / "oap-server" / "server-starter" / "src" / "main" / "resources" / "bydb-topn.yml"
    if not path.exists():
        return []
    rules: list[TopNRule] = []
    current: dict[str, object] | None = None
    in_group_by = False
    for line_number, line in enumerate(path.read_text(encoding="utf-8", errors="ignore").splitlines(), 1):
        if re.match(r"\s*-\s+name:\s*", line):
            if current and current.get("name") and current.get("metricName"):
                rules.append(
                    TopNRule(
                        name=str(current["name"]),
                        metric_name=str(current["metricName"]),
                        group_by=list(current.get("groupByTagNames", [])),
                        sort=str(current.get("sort", "all")),
                        path=path,
                        line_number=int(current.get("line_number", line_number)),
                    )
                )
            current = {"groupByTagNames": [], "line_number": line_number}
            current["name"] = line.split(":", 1)[1].strip()
            in_group_by = False
            continue
        if current is None:
            continue
        metric_match = re.match(r"\s*metricName:\s*(\S+)", line)
        sort_match = re.match(r"\s*sort:\s*(\S+)", line)
        if metric_match:
            current["metricName"] = metric_match.group(1)
            in_group_by = False
        elif sort_match:
            current["sort"] = sort_match.group(1)
            in_group_by = False
        elif re.match(r"\s*groupByTagNames:\s*$", line):
            in_group_by = True
        elif in_group_by:
            tag_match = re.match(r"\s*-\s+(\S+)", line)
            if tag_match:
                current.setdefault("groupByTagNames", []).append(tag_match.group(1))
            elif line.strip() and not line.strip().startswith("#"):
                in_group_by = False
    if current and current.get("name") and current.get("metricName"):
        rules.append(
            TopNRule(
                name=str(current["name"]),
                metric_name=str(current["metricName"]),
                group_by=list(current.get("groupByTagNames", [])),
                sort=str(current.get("sort", "all")),
                path=path,
                line_number=int(current.get("line_number", 0)),
            )
        )
    return rules


def metric_resources(metric_name: str) -> str:
    return ", ".join(f"`{metric_name}_{suffix}` in `{group_name(group)}`" for suffix, group in ((METRIC_RESOURCE_SUFFIXES[key], METRIC_GROUPS[key]) for key in ("minute", "hour", "day")))


def table_row(values: list[str]) -> str:
    return "| " + " | ".join(value.replace("\n", " ").replace("|", "\\|") for value in values) + " |"


def render_storage_catalog(rows: list[dict[str, str]], classes: dict[str, ModelInfo], skywalking_root: Path, source_label: str) -> str:
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    lines = [
        "# Generated SkyWalking BanyanDB Storage Catalog",
        "",
        f"Generated by `extract_skywalking_storage.py` from `{source_label}` at `{generated_at}`.",
        "",
        "This is a static source-code catalog. Always confirm exact groups, resources, tags, and fields with live BanyanDB schema discovery before executing BydbQL.",
        "",
        "## Group Rules",
        "",
        "- Namespace default: `sw`, producing groups such as `sw_metricsMinute`, `sw_recordsLog`, and `sw_trace`.",
        "- Measures from OAL/MAL metrics usually exist as `<metric>_minute`, `<metric>_hour`, and `<metric>_day` in `sw_metricsMinute`, `sw_metricsHour`, and `sw_metricsDay` when the corresponding downsampling is enabled.",
        "- Index-mode metadata measures use `sw_metadata`.",
        "- Streams use `sw_records`, `sw_recordsLog`, or `sw_recordsBrowserErrorLog` from `@BanyanDB.Group(streamGroup=...)`.",
        "- Traces use `sw_trace` or `sw_zipkinTrace` from `@BanyanDB.Group(traceGroup=...)`.",
        "- Properties use `sw_property`.",
        "",
        "## High-Value Resources",
        "",
        "- Logs: `SELECT * FROM STREAM log IN sw_recordsLog TIME > '-15m' LIMIT 20`.",
        "- SkyWalking traces: `SELECT () FROM TRACE segment IN sw_trace TIME > '-30m' LIMIT 20` for raw storage inspection only.",
        "- Metrics: `SELECT * FROM MEASURE service_cpm_minute IN sw_metricsMinute TIME > '-30m' LIMIT 20`.",
        "",
        "## Model-Derived Resources",
        "",
        table_row(["Kind", "Group", "Resource(s)", "Model", "Timestamp", "Series ID", "Fields/Tags", "Sortable", "Match Analyzers", "Source"]),
        table_row(["---", "---", "---", "---", "---", "---", "---", "---", "---", "---"]),
    ]
    for row in rows:
        lines.append(
            table_row(
                [
                    row["kind"],
                    f"`{row['group']}`",
                    ", ".join(f"`{item.strip()}`" for item in row["resources"].split(",") if item.strip()),
                    row["model"],
                    f"`{row['timestamp']}`" if row["timestamp"] else "",
                    row["series"],
                    row["fields"],
                    row["sortable"],
                    row["match"],
                    f"`{relative(Path(row['source']), skywalking_root)}`",
                ]
            )
        )
    return "\n".join(lines).rstrip() + "\n"


def render_metrics_catalog(metrics: list[MetricDefinition], skywalking_root: Path, source_label: str) -> str:
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    lines = [
        "# Generated SkyWalking Metrics Catalog",
        "",
        f"Generated by `extract_skywalking_storage.py` from OAL/MAL resources under `{source_label}` at `{generated_at}`.",
        "",
        "Metric resource names are source-derived conventions. Confirm existence with live BanyanDB schema before querying.",
        "",
        "## Metrics",
        "",
        table_row(["Metric", "Family", "Source expression", "Function", "BanyanDB resources", "Source"]),
        table_row(["---", "---", "---", "---", "---", "---"]),
    ]
    for metric in sorted(metrics, key=lambda item: (item.name, item.family, str(item.path))):
        lines.append(
            table_row(
                [
                    f"`{metric.name}`",
                    metric.family,
                    f"`{metric.source}`",
                    f"`{metric.function}`",
                    metric_resources(metric.name),
                    f"`{relative(metric.path, skywalking_root)}:{metric.line_number}`",
                ]
            )
        )
    return "\n".join(lines).rstrip() + "\n"


def render_topn(rules: list[TopNRule], skywalking_root: Path, source_label: str) -> str:
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    lines = [
        "# Generated BanyanDB TopN Rules",
        "",
        f"Generated by `extract_skywalking_storage.py` from `bydb-topn.yml` in `{source_label}` at `{generated_at}`.",
        "",
        "Use these names to understand OAP TopN behavior. For raw BydbQL ranking, prefer `SHOW TOP` against the source measure and verify available TopN aggregations with live schema when needed.",
        "",
        table_row(["Rule", "Metric", "Group By Tags", "Sort", "Expected Measure Resources", "Source"]),
        table_row(["---", "---", "---", "---", "---", "---"]),
    ]
    for rule in rules:
        lines.append(
            table_row(
                [
                    f"`{rule.name}`",
                    f"`{rule.metric_name}`",
                    ", ".join(f"`{tag}`" for tag in rule.group_by) or "",
                    f"`{rule.sort}`",
                    metric_resources(rule.metric_name),
                    f"`{relative(rule.path, skywalking_root)}:{rule.line_number}`",
                ]
            )
        )
    return "\n".join(lines).rstrip() + "\n"


def render_entity_ids(skywalking_root: Path, source_label: str) -> str:
    id_manager = skywalking_root / "oap-server" / "server-core" / "src" / "main" / "java" / "org" / "apache" / "skywalking" / "oap" / "server" / "core" / "analysis" / "IDManager.java"
    generated_at = dt.datetime.now(dt.UTC).replace(microsecond=0).isoformat()
    source = relative(id_manager, skywalking_root)
    return f"""# SkyWalking Entity IDs

Generated from `{source}` in `{source_label}` at `{generated_at}`.

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
"""


def write_outputs(source: SourceRoot, output_dir: Path) -> None:
    classes = parse_java_models(source.path)
    rows = storage_rows(classes)
    metrics = parse_oal_metrics(source.path) + parse_mal_metrics(source.path)
    topn_rules = parse_topn_rules(source.path)
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "generated-storage-catalog.md").write_text(render_storage_catalog(rows, classes, source.path, source.label), encoding="utf-8")
    (output_dir / "generated-metrics-catalog.md").write_text(render_metrics_catalog(metrics, source.path, source.label), encoding="utf-8")
    (output_dir / "generated-topn.md").write_text(render_topn(topn_rules, source.path, source.label), encoding="utf-8")
    (output_dir / "entity-ids.md").write_text(render_entity_ids(source.path, source.label), encoding="utf-8")


def default_output_dir() -> Path:
    return Path(__file__).resolve().parents[1] / "references"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--skywalking-root", type=Path, default=None, help="Optional local SkyWalking source checkout. If omitted, download GitHub archive.")
    parser.add_argument("--github-repo", default=DEFAULT_GITHUB_REPO, help="GitHub repository to download when --skywalking-root is omitted.")
    parser.add_argument("--github-ref", default=DEFAULT_GITHUB_REF, help="GitHub branch, tag, or SHA to download when --skywalking-root is omitted.")
    parser.add_argument("--github-cache", type=Path, default=None, help="Optional directory for caching downloaded GitHub archives.")
    parser.add_argument("--output-dir", type=Path, default=default_output_dir(), help="Reference output directory.")
    args = parser.parse_args()
    with open_skywalking_source(args.skywalking_root, args.github_repo, args.github_ref, args.github_cache) as source:
        write_outputs(source, args.output_dir)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
