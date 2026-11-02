#!/usr/bin/env python3
"""Compile the same test overlay against archived and working sources, then run paced A/B serially."""
from __future__ import annotations
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import statistics
import subprocess
import time

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
BASELINE_COMMIT = "29b609bc4424e89b68a7f50809add3b1eef5637d"
SOURCES = ["services/execution/runtime_events.go", "internal/chat/eventstream/live_adapter.go", "internal/chat/eventstream/adapter.go", "internal/chat/messageview/message_view.go"]


def command(args: list[str], cwd: Path | None = None, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(args, cwd=cwd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError(f"command exited {result.returncode}: {' '.join(args)}\n{result.stdout}")
    return result.stdout


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def production_tree_digest(server: Path) -> str:
    state = hashlib.sha256()
    paths = sorted(path for path in server.rglob("*.go") if not path.name.endswith("_test.go"))
    paths += [server / "go.mod", server / "go.sum"]
    for path in paths:
        state.update(str(path.relative_to(server)).encode())
        state.update(b"\0")
        state.update(bytes.fromhex(digest(path)))
    return state.hexdigest()


def prepare(options: argparse.Namespace) -> dict:
    options.work.mkdir(parents=True, exist_ok=True)
    variants = {}
    for variant, server in (("baseline", options.baseline), ("current", options.current / "server")):
        protocol = HERE / f"protocol-{variant}.go.txt"
        overlay = {"Replace": {str(server / "services/execution/performance_ab_overlay_test.go"): str(HERE / "runtime_sink_test.go"), str(server / "services/execution/performance_ab_protocol_overlay_test.go"): str(protocol)}}
        overlay_path = options.work / f"{variant}.overlay.json"
        overlay_path.write_text(json.dumps(overlay, indent=2))
        variants[variant] = {"server": str(server), "overlay": str(overlay_path), "binary": str(options.work / f"{variant}.test"), "protocol_sha256": digest(protocol), "production_source_sha256": {name: digest(server / name) for name in SOURCES}, "production_tree_sha256": production_tree_digest(server)}
    for name in SOURCES:
        expected = subprocess.check_output(["git", "show", f"{BASELINE_COMMIT}:server/{name}"], cwd=options.current)
        if hashlib.sha256(expected).hexdigest() != variants["baseline"]["production_source_sha256"][name]:
            raise RuntimeError(f"archive is not the requested old production source: {name}")
    return {"baseline_commit": BASELINE_COMMIT, "harness_sha256": digest(HERE / "runtime_sink_test.go"), "variants": variants}


def build(options: argparse.Namespace, manifest: dict) -> None:
    env = os.environ.copy()
    env.update({"GOCACHE": "/private/tmp/neuxbot-chat-go-cache", "GOWORK": "off"})
    go_root = options.go_root or Path(command(["go", "env", "GOROOT"], cwd=options.current / "server", env=env).strip())
    env.update({"GOROOT": str(go_root), "GOTOOLCHAIN": "local"})
    for variant, values in manifest["variants"].items():
        output = command([str(go_root / "bin/go"), "test", "-c", "-overlay", values["overlay"], "-o", values["binary"], "./services/execution"], cwd=Path(values["server"]), env=env)
        print(f"compiled {variant}: {output.strip()}", flush=True)
    (options.work / "build-manifest.json").write_text(json.dumps(manifest, indent=2))


def verify_frozen(manifest: dict, built: dict) -> None:
    if manifest != built:
        raise RuntimeError("overlay or relevant production source changed after build; rebuild explicitly")


def sample(options: argparse.Namespace, manifest: dict, variant: str, scenario: str, repeat: int, interval: int) -> dict:
    output_path = options.work / f"{variant}-{scenario}-{repeat}.json"
    canonical_path = options.work / f"{variant}-{scenario}-{repeat}.canonical.json"
    values = manifest["variants"][variant]
    env = os.environ.copy()
    env.update({"NEUX_CHAT_AB_ENABLE": "1", "NEUX_CHAT_AB_SCENARIO": scenario, "NEUX_CHAT_AB_REPEAT": str(repeat), "NEUX_CHAT_AB_INTERVAL_MS": str(interval), "NEUX_CHAT_AB_OUTPUT": str(output_path), "NEUX_CHAT_AB_CANONICAL_OUTPUT": str(canonical_path)})
    started = time.time()
    output = command([values["binary"], "-test.run", "^TestBackendPerformanceAB$", "-test.count=1", "-test.v"], cwd=Path(values["server"]), env=env)
    document = json.loads(output_path.read_text())
    document["started_unix_seconds"] = started
    document["runner_elapsed_seconds"] = time.time() - started
    document["test_stdout"] = output
    print(f"{variant} {scenario} repeat={repeat} completed", flush=True)
    return document


def percentile(values: list[float], quantile: float) -> float:
    return sorted(values)[max(0, math.ceil(quantile * len(values)) - 1)]


def summarize(documents: list[dict]) -> dict:
    groups = {}
    for document in documents:
        row = document["samples"][0]
        phase, traffic = row["measured"], row["measured"]["traffic"]
        values = {"cpu_ms": phase["userCpuMs"] + phase["systemCpuMs"], "user_cpu_ms": phase["userCpuMs"], "system_cpu_ms": phase["systemCpuMs"], "alloc_bytes": phase["allocBytes"], "mallocs": phase["mallocs"], "elapsed_ms": phase["elapsedMs"], "serialize_ms": (traffic["liveSerializeNs"] + traffic["durableSerializeNs"]) / 1e6, "wire_bytes": traffic["liveWireBytes"] + traffic["durableWireBytes"], "live_wire_bytes": traffic["liveWireBytes"], "durable_wire_bytes": traffic["durableWireBytes"], "body_bytes": traffic["liveBodyBytes"] + traffic["durableBodyBytes"], "body_multiple": (traffic["liveBodyBytes"] + traffic["durableBodyBytes"]) / row["inputBodyBytes"], "live_body_bytes": traffic["liveBodyBytes"], "durable_body_bytes": traffic["durableBodyBytes"], "live_frames": traffic["liveFrames"], "durable_frames": traffic["durableFrames"], "live_tool_parts": traffic["liveToolParts"], "durable_tool_parts": traffic["durableToolParts"], "max_source_delay_ms": row["maxSourceDelayMs"]}
        key = f"{row['scenario']}:{row['variant']}"
        group = groups.setdefault(key, {})
        for metric, value in values.items():
            group.setdefault(metric, []).append(value)
    return {key: {metric: {"raw": values, "median": statistics.median(values), "p95_nearest_rank": percentile(values, .95), "max": max(values)} for metric, values in metrics.items()} for key, metrics in groups.items()}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["prepare", "build", "smoke", "run"])
    parser.add_argument("--baseline", type=Path, default=Path("/private/tmp/neux-chat-ab-baseline/server"))
    parser.add_argument("--current", type=Path, default=ROOT)
    parser.add_argument("--work", type=Path, default=Path("/private/tmp/neux-chat-backend-ab"))
    parser.add_argument("--output", type=Path, default=Path("/private/tmp/neux-chat-backend-ab.json"))
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--go-root", type=Path, default=Path(os.environ["GOROOT"]) if os.environ.get("GOROOT") else None)
    options = parser.parse_args()
    options.baseline, options.current, options.work = options.baseline.resolve(), options.current.resolve(), options.work.resolve()
    manifest = prepare(options)
    if options.mode == "prepare":
        print(json.dumps(manifest, indent=2)); return
    if options.mode == "build":
        build(options, manifest); return
    built = json.loads((options.work / "build-manifest.json").read_text())
    verify_frozen(manifest, built)
    count = 1 if options.mode == "smoke" else options.repetitions
    interval = 1 if options.mode == "smoke" else 20
    documents, order = [], []
    for scenario in ["plain", "tools100"]:
        for repeat in range(1, count + 1):
            variants = ["baseline", "current"] if repeat % 2 else ["current", "baseline"]
            for variant in variants:
                order.append({"variant": variant, "scenario": scenario, "repeat": repeat})
                documents.append(sample(options, manifest, variant, scenario, repeat, interval))
                interim = {"mode": options.mode, "manifest": manifest, "schedule": order, "documents": documents, "measurement_notes": ["Actual archived baseline and working production runtimeEventSink, eventlog.Store and eventstream.Adapter; no product changes.", "This measures the backend producer/public serialization boundary; subscriptionHub/global WS queues, sockets, Electron and renderer work are not included.", "Source input is 50KiB/100 events; scheduled interval is 20ms in measured runs, 1ms smoke is semantic validation only.", "Tool establishment is a separately reported warmup; measured phase starts with 100 completed tools and includes all subsequent text, Flush and terminal publication.", "Both actual durable full-message publications remain counted. Body effective bytes count Content plus text display parts, and incremental text exactly once.", "CPU and allocations include production folding, fsync, public projection, JSON serialization, and allocation-free typed counter telemetry; exclude fixture setup, explicit pre-phase GC, and post-phase semantic hashing.", "Encoded payloads are not retained; heap start/end values are not a peak measurement.", "Independent workspace capture is not included: the baseline did not provide the same feature, so its cost must be reported separately.", "Canonical final-message comparison excludes createdAt/updatedAt, runMetrics.startedAt/durationMs and protocol-only liveRevision; all body, part, identity, status and remaining metadata fields are compared.", "p95 uses nearest-rank and with five repetitions is the maximum observed value."]}
                options.output.write_text(json.dumps(interim, indent=2))
    for scenario in ["plain", "tools100"]:
        hashes = {d["samples"][0]["finalMessageSha256"] for d in documents if d["samples"][0]["scenario"] == scenario}
        if len(hashes) != 1:
            raise RuntimeError(f"final semantic messages differ for {scenario}; compare canonical JSON files in {options.work}")
    interim["semantic_equality_passed"] = True
    interim["summary"] = summarize(documents)
    options.output.write_text(json.dumps(interim, indent=2))
    print(f"wrote {options.output}; semantic equality passed", flush=True)


if __name__ == "__main__":
    main()
