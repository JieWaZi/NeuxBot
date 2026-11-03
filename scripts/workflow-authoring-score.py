#!/usr/bin/env python3
"""按冻结案例重算人工评分清单；不执行模型，也不自动判断语义。

python3 scripts/workflow-authoring-score.py --cases cases.frozen.json \
    --manifest R1-grading.json --output R1-score.json

输入兼容逐例 automatic_checks/semantic_checks 的评分清单。缺项为 not_run；
预计算 score/status/assessed_weight 不作为事实输入。证据引用只校验非空，
证据内容和人工语义分必须由评分员核对。aggregate_kind 省略或 single_round
沿用单轮门槛；cross_round_mixed_evidence 只输出跨轮数字画像，算术门槛
单列 rubric_gate，gate=not_applicable_cross_round，release_gate=not_evaluated。
混合画像即使数值达线也不表示同版本全套通过。退出码 0 仅表示计算成功，
2 表示输入非法，不表示模型测试或发布通过。
"""

import argparse
import hashlib
import json
import math
from pathlib import Path
import sys


def require(condition, message):
    if not condition:
        raise ValueError(message)


def reject_nonfinite(value):
    """忽略的派生字段也不允许藏入 NaN/Infinity。"""
    if isinstance(value, float):
        require(math.isfinite(value), "non-finite number is not allowed")
    elif isinstance(value, dict):
        for child in value.values():
            reject_nonfinite(child)
    elif isinstance(value, list):
        for child in value:
            reject_nonfinite(child)


def read_json(path):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "duplicate JSON key: " + key)
            result[key] = value
        return result

    def constant(value):
        raise ValueError("invalid JSON number: " + value)

    return json.loads(Path(path).read_text(encoding="utf-8"), object_pairs_hook=pairs, parse_constant=constant)


def indexed(items, key, allowed, label):
    require(isinstance(items, list), label + " must be an array")
    result = {}
    for item in items:
        require(isinstance(item, dict), label + " entry must be an object")
        identity = item.get(key)
        require(type(identity) in (str, int), label + " identity has invalid type")
        require(identity in allowed, "unknown " + label + ": " + str(identity))
        require(identity not in result, "duplicate " + label + ": " + str(identity))
        result[identity] = item
    return result


def evidence(item, label):
    refs = item.get("evidence")
    require(isinstance(refs, list) and bool(refs), label + " requires evidence")
    require(all(isinstance(ref, str) and bool(ref.strip()) for ref in refs), label + " has invalid evidence")
    return refs


def number(value, label):
    require(type(value) in (int, float) and math.isfinite(value), label + " must be a finite number")
    return value


def score_manifest(suite, manifest):
    reject_nonfinite(suite)
    reject_nonfinite(manifest)
    require(isinstance(suite, dict) and isinstance(manifest, dict), "suite and manifest must be objects")
    aggregate_kind = manifest.get("aggregate_kind", "single_round")
    require(isinstance(aggregate_kind, str) and aggregate_kind in ("single_round", "cross_round_mixed_evidence"),
            "unsupported aggregate_kind")
    rules = suite.get("scoring", {})
    require(rules.get("automatic_fraction") == 0.6 and rules.get("semantic_fraction") == 0.4,
            "this scorer supports only the frozen 60/40 rules")
    require(rules.get("total_weight") == 100, "frozen total weight must be 100")
    require(manifest.get("suite_id", suite.get("suite_id")) == suite.get("suite_id"), "suite_id mismatch")
    definitions = suite.get("cases")
    require(isinstance(definitions, list) and bool(definitions), "suite cases must be a nonempty array")
    ids = [c.get("id") for c in definitions if isinstance(c, dict)]
    require(len(ids) == len(definitions) and all(isinstance(i, str) and bool(i) for i in ids), "invalid suite case identity")
    require(len(set(ids)) == len(ids), "duplicate suite case")
    require(sum(number(c.get("weight"), "weight") for c in definitions) == 100, "case weights must total 100")
    provided = indexed(manifest.get("cases", []), "id", ids, "case")
    cases, hard_failures = [], []
    for definition in definitions:
        case_id = definition["id"]
        weight = definition["weight"]
        require(weight > 0, "case weight must be positive")
        item = provided.get(case_id, {})
        auto_defs = definition.get("automatic_checks")
        semantic_defs = definition.get("semantic_checks")
        require(isinstance(auto_defs, list) and bool(auto_defs), "case needs automatic check definitions")
        require(isinstance(semantic_defs, list) and bool(semantic_defs), "case needs semantic check definitions")
        auto_ids = [a.get("id") for a in auto_defs if isinstance(a, dict)]
        require(len(auto_ids) == len(auto_defs) and all(isinstance(i, str) and i for i in auto_ids)
                and len(set(auto_ids)) == len(auto_ids), "invalid or duplicate suite automatic check")
        automatic = indexed(item.get("automatic_checks", []), "id", auto_ids, "automatic check")
        semantic = indexed(item.get("semantic_checks", []), "index", range(1, len(semantic_defs) + 1), "semantic check")
        auto_weight = weight * 0.6 / len(auto_defs)
        semantic_weight = weight * 0.4 / len(semantic_defs)
        earned, assessed, auto_results, semantic_results = 0, 0, [], []
        for check in auto_defs:
            grade = automatic.get(check["id"], {})
            status = grade.get("status", "not_run")
            require(status in ("pass", "fail", "not_run"), "invalid automatic status")
            value = {"pass": 1, "fail": 0, "not_run": None}[status]
            require("value" not in grade or (type(grade["value"]) is type(value) and grade["value"] == value),
                    "automatic value contradicts status")
            if "layer" in grade:
                require(grade["layer"] == check.get("layer"), "automatic layer mismatch")
            refs = evidence(grade, check["id"]) if value is not None else grade.get("evidence", [])
            if value is not None:
                assessed += auto_weight
                earned += auto_weight * value
            auto_results.append({"id": check["id"], "status": status, "value": value,
                                 "evidence": refs, "reason": grade.get("reason", "")})
        for index in range(1, len(semantic_defs) + 1):
            grade = semantic.get(index, {})
            value = grade.get("score")
            if value is not None:
                require(type(value) is int and 0 <= value <= 4, "semantic score must be an integer in 0..4")
                require(grade.get("status", "scored") == "scored", "semantic status contradicts score")
                refs = evidence(grade, case_id + " semantic " + str(index))
                require(isinstance(grade.get("reason"), str) and bool(grade["reason"].strip()), "semantic score requires human rationale")
                assessed += semantic_weight
                earned += semantic_weight * value / 4
            else:
                require(grade.get("status", "not_run") == "not_run", "missing semantic score must be not_run")
                refs = grade.get("evidence", [])
            semantic_results.append({"index": index, "status": "not_run" if value is None else "scored",
                                     "score": value, "evidence": refs, "reason": grade.get("reason", "")})
        failures = item.get("hard_failures_observed", [])
        require(isinstance(failures, list), "hard_failures_observed must be an array")
        seen = set()
        for failure in failures:
            require(isinstance(failure, dict), "hard failure needs {failure,evidence}")
            text = failure.get("failure")
            require(isinstance(text, str) and text in definition.get("hard_failures", []), "unknown hard failure")
            require(text not in seen, "duplicate hard failure")
            seen.add(text)
            hard_failures.append({"case_id": case_id, "failure": text, "evidence": evidence(failure, "hard failure")})
        missing = round(weight - assessed, 8)
        score = None if missing > 0 else round(earned, 8)
        floor_pass = None if score is None else score >= weight * 0.6
        status = "not_run" if assessed == 0 else ("partial" if score is None else ("pass" if floor_pass and not seen else "fail"))
        cases.append({"id": case_id, "weight": weight, "holdout": definition.get("holdout", False),
                      "status": status, "score": score, "earned_points": round(earned, 8),
                      "assessed_weight": round(assessed, 8), "unassessed_weight": missing,
                      "case_floor_pass": floor_pass, "automatic_checks": auto_results,
                      "semantic_checks": semantic_results})
    earned = round(sum(c["earned_points"] for c in cases), 8)
    assessed = round(sum(c["assessed_weight"] for c in cases), 8)
    complete = all(c["score"] is not None for c in cases)
    failed = bool(hard_failures) or any(c["case_floor_pass"] is False for c in cases)
    gate = "fail" if failed or (complete and earned < 85) else ("pass" if complete else "not_passed")
    status = "not_run" if assessed == 0 else ("partial" if not complete else ("pass" if gate == "pass" else "fail"))
    result = {"schema_version": 1, "suite_id": suite.get("suite_id"), "round": manifest.get("round"),
              "aggregate_kind": aggregate_kind,
            "status": status, "score": earned if complete else None, "gate": gate,
            "earned_points": earned, "assessed_weight": assessed, "unassessed_weight": round(100 - assessed, 8),
            "not_normalized": True, "semantic_scores_are_manual": True,
            "hard_failures_observed": hard_failures, "cases": cases}
    if aggregate_kind == "cross_round_mixed_evidence":
        # 跨轮样本的数值达线不能证明任一固定版本全套或发布门槛通过。
        result.update(rubric_gate=gate, gate="not_applicable_cross_round", release_gate="not_evaluated",
                      status="complete" if complete else status,
                      warning="跨轮混合证据画像；数字与rubric_gate不代表同版本全套成绩或发布通过。")
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--cases", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    try:
        require(not args.output or args.output.resolve() not in (args.cases.resolve(), args.manifest.resolve()),
                "output must not overwrite cases or manifest")
        suite, manifest = read_json(args.cases), read_json(args.manifest)
        digest = hashlib.sha256(args.cases.read_bytes()).hexdigest()
        require(manifest.get("frozen_cases_sha256", digest) == digest, "frozen cases digest mismatch")
        result = score_manifest(suite, manifest)
        result["frozen_cases_sha256"] = digest
        output = json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n"
        if args.output:
            args.output.write_text(output, encoding="utf-8")
        else:
            print(output, end="")
        return 0
    except (ValueError, TypeError, KeyError, OSError) as error:
        print("Invalid scoring input: " + str(error), file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
