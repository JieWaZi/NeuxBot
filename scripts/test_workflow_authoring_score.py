"""评分器定向测试：python3 -m unittest discover -s scripts -p test_workflow_authoring_score.py。"""

import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("workflow-authoring-score.py")


class ScoreTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.exists(), "repeatable scorer is missing")
        spec = importlib.util.spec_from_file_location("workflow_authoring_score", SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.suite = {
            "suite_id": "example-v1",
            "scoring": {"automatic_fraction": 0.6, "semantic_fraction": 0.4, "total_weight": 100},
            "cases": [self.case("A", 40), self.case("B", 60)],
        }
        self.manifest = {"cases": [self.grade("A"), self.grade("B")]}

    @staticmethod
    def case(case_id, weight):
        return {"id": case_id, "weight": weight, "holdout": False,
                "automatic_checks": [{"id": case_id + "-1"}, {"id": case_id + "-2"}],
                "semantic_checks": ["判断符合材料", "说明边界"], "hard_failures": ["虚构证据"]}

    @staticmethod
    def grade(case_id):
        return {"id": case_id,
                "automatic_checks": [{"id": case_id + "-" + str(i), "status": "pass", "evidence": ["trace#/" + str(i)]} for i in [1, 2]],
                "semantic_checks": [{"index": i, "score": 4, "evidence": ["transcript#/" + str(i)], "reason": "人工核对原始材料"} for i in [1, 2]],
                "hard_failures_observed": []}

    def score(self):
        return self.module.score_manifest(self.suite, self.manifest)

    def test_full_evidence_scores_100(self):
        result = self.score()
        self.assertEqual((result["status"], result["score"], result["gate"]), ("pass", 100, "pass"))
        self.assertEqual(result["unassessed_weight"], 0)

    def test_partial_never_normalizes_or_turns_not_run_to_zero(self):
        self.manifest["cases"] = [{"id": "A", "automatic_checks": [
            {"id": "A-1", "status": "pass", "evidence": ["trace#/1"]},
            {"id": "A-2", "status": "not_run", "value": None}],
            "semantic_checks": [{"index": 1, "score": 4, "evidence": ["trace#/2"], "reason": "人工核验"}]}]
        result = self.score()
        self.assertIsNone(result["score"])
        self.assertEqual(result["status"], "partial")
        self.assertEqual((result["earned_points"], result["assessed_weight"], result["unassessed_weight"]), (20, 20, 80))
        self.assertIsNone(result["cases"][0]["automatic_checks"][1]["value"])

    def test_empty_manifest_is_not_run_with_null_score(self):
        self.manifest = {"cases": []}
        result = self.score()
        self.assertEqual(result["status"], "not_run")
        self.assertIsNone(result["score"])
        self.assertEqual(result["unassessed_weight"], 100)

    def test_hard_failure_blocks_even_100(self):
        self.manifest["cases"][0]["hard_failures_observed"] = [{"failure": "虚构证据", "evidence": ["trace#/forgery"]}]
        result = self.score()
        self.assertEqual(result["score"], 100)
        self.assertEqual((result["status"], result["gate"]), ("fail", "fail"))

    def test_case_floor_blocks_total_above_85(self):
        self.suite["cases"][0]["weight"] = 10
        self.suite["cases"][1]["weight"] = 90
        for check in self.manifest["cases"][0]["automatic_checks"]:
            check["status"] = "fail"
        result = self.score()
        self.assertEqual(result["score"], 94)
        self.assertEqual(result["gate"], "fail")

    def test_rejects_unknown_duplicate_and_invalid_ids(self):
        edits = [
            lambda x: x["cases"].append(copy.deepcopy(x["cases"][0])),
            lambda x: x["cases"][0].update(id="UNKNOWN"),
            lambda x: x["cases"][0]["automatic_checks"][0].update(id="B-1"),
            lambda x: x["cases"][0]["automatic_checks"].append(copy.deepcopy(x["cases"][0]["automatic_checks"][0])),
            lambda x: x["cases"][0]["semantic_checks"].append(copy.deepcopy(x["cases"][0]["semantic_checks"][0])),
            lambda x: x["cases"][0]["semantic_checks"][0].update(index=0),
            lambda x: x["cases"][0]["hard_failures_observed"].append({"failure": "未知门槛", "evidence": ["x"]}),
        ]
        for edit in edits:
            with self.subTest(edit=edit):
                altered = copy.deepcopy(self.manifest)
                edit(altered)
                with self.assertRaises(ValueError):
                    self.module.score_manifest(self.suite, altered)

    def test_rejects_missing_evidence_invalid_score_and_not_run_zero(self):
        edits = [
            lambda x: x["cases"][0]["automatic_checks"][0].update(evidence=[]),
            lambda x: x["cases"][0]["semantic_checks"][0].update(evidence=[]),
            lambda x: x["cases"][0]["semantic_checks"][0].update(score=5),
            lambda x: x["cases"][0]["semantic_checks"][0].update(score=-1),
            lambda x: x["cases"][0]["semantic_checks"][0].update(score=True),
            lambda x: x["cases"][0]["automatic_checks"][0].update(status="not_run", value=0),
            lambda x: x["cases"][0]["automatic_checks"][0].update(status="maybe"),
            lambda x: x["cases"][0].update(score=float("nan")),
            lambda x: x["cases"][0].update(score=float("inf")),
        ]
        for edit in edits:
            with self.subTest(edit=edit):
                altered = copy.deepcopy(self.manifest)
                edit(altered)
                with self.assertRaises(ValueError):
                    self.module.score_manifest(self.suite, altered)

    def test_cli_recomputes_derived_totals_without_touching_manifest(self):
        self.manifest["score"] = 2
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            cases, manifest, output = [root / x for x in ["cases.json", "input.json", "output.json"]]
            cases.write_text(json.dumps(self.suite))
            manifest.write_text(json.dumps(self.manifest))
            original = manifest.read_bytes()
            run = subprocess.run([sys.executable, str(SCRIPT), "--cases", str(cases), "--manifest", str(manifest), "--output", str(output)], capture_output=True, text=True)
            self.assertEqual(run.returncode, 0, run.stderr)
            self.assertEqual(json.loads(output.read_text())["score"], 100)
            self.assertEqual(manifest.read_bytes(), original)
            invalid = subprocess.run([sys.executable, str(SCRIPT), "--cases", str(cases), "--manifest", str(manifest), "--output", str(manifest)], capture_output=True, text=True)
            self.assertNotEqual(invalid.returncode, 0)
            self.assertEqual(manifest.read_bytes(), original)

    def test_cli_mixed_profile_never_claims_release_pass(self):
        self.manifest["aggregate_kind"] = "cross_round_mixed_evidence"
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            cases, manifest = root / "cases.json", root / "manifest.json"
            cases.write_text(json.dumps(self.suite))
            manifest.write_text(json.dumps(self.manifest))
            run = subprocess.run([sys.executable, str(SCRIPT), "--cases", str(cases), "--manifest", str(manifest)], capture_output=True, text=True)
            self.assertEqual(run.returncode, 0, run.stderr)
            result = json.loads(run.stdout)
            self.assertEqual(result["score"], 100)
            self.assertEqual(result["gate"], "not_applicable_cross_round")
            self.assertEqual(result["aggregate_kind"], "cross_round_mixed_evidence")
            self.assertEqual(result["rubric_gate"], "pass")
            self.assertEqual(result["release_gate"], "not_evaluated")
            self.assertEqual(result["status"], "complete")

    def test_mixed_partial_retains_missing_scores_and_hard_failure_rubric(self):
        self.manifest["aggregate_kind"] = "cross_round_mixed_evidence"
        self.manifest["cases"] = [{"id": "A", "hard_failures_observed": [
            {"failure": "虚构证据", "evidence": ["trace#/forgery"]}]}]
        result = self.score()
        self.assertIsNone(result["score"])
        self.assertEqual(result["rubric_gate"], "fail")
        self.assertEqual(result["gate"], "not_applicable_cross_round")
        self.assertEqual(result["release_gate"], "not_evaluated")
        self.assertEqual(result["unassessed_weight"], 100)

    def test_aggregate_kind_is_validated_and_single_round_remains_compatible(self):
        self.manifest["aggregate_kind"] = "single_round"
        self.assertEqual(self.score()["gate"], "pass")
        for kind in ["mixed", "release_pass", None, [], 1]:
            with self.subTest(kind=kind):
                self.manifest["aggregate_kind"] = kind
                with self.assertRaises(ValueError):
                    self.score()

    def test_json_reader_rejects_nan_and_duplicate_keys(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "input.json"
            for content in ['{"score":NaN}', '{"score":Infinity}', '{"cases":[],"cases":[]}']:
                with self.subTest(content=content):
                    path.write_text(content)
                    with self.assertRaises(ValueError):
                        self.module.read_json(path)


if __name__ == "__main__":
    unittest.main()
