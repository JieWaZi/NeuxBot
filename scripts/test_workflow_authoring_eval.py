"""运行：python3 -m unittest discover -s scripts -p test_workflow_authoring_eval.py。"""

import importlib.util
import json
from pathlib import Path
import tempfile
import threading
import unittest
from contextlib import redirect_stdout, redirect_stderr
import io
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


SCRIPT = Path(__file__).with_name("workflow-authoring-eval.py")


class EvalTests(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.exists(), "the repeatable evaluation driver is missing")
        spec = importlib.util.spec_from_file_location("workflow_authoring_eval", SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.requests = []
        self.creates = {}
        self.sends = {}
        self.drop_path = None
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
                owner.requests.append(("POST", self.path, body, dict(self.headers)))
                if self.path == "/api/web/session":
                    self.reply({"csrfToken": "private-csrf"}, cookie=True)
                    return
                if self.headers.get("X-Ally-CSRF") != "private-csrf" or "session=private-cookie" not in self.headers.get("Cookie", ""):
                    self.reply({}, status=403)
                    return
                if self.path == "/api/v1/workflows":
                    owner.creates.setdefault(body["requestId"], {"id": "definition1"})
                    result = owner.creates[body["requestId"]]
                elif self.path == "/api/v1/workflows/definition1/creation/messages":
                    owner.sends.setdefault(body["requestId"], {"id": "session1", "chatId": "chat1", "acceptedExecutionId": "execution1"})
                    result = owner.sends[body["requestId"]]
                else:
                    self.reply({}, status=404)
                    return
                if owner.drop_path == self.path:
                    owner.drop_path = None
                    self.close_connection = True
                    self.connection.close()
                    return
                self.reply(result)

            def do_GET(self):
                owner.requests.append(("GET", self.path, None, dict(self.headers)))
                responses = {
                    "/api/v1/workflows/definition1": {"definition": {"id": "definition1"}, "creationSessions": [{"id": "session1", "chatId": "chat1"}]},
                    "/api/v1/workflows/definition1/source": {"source": "export const meta = {};", "revisionId": "revision1"},
                    "/api/v1/workflows/definition1/events/history?after=0": {"resources": [{"type": "workflow.revision.saved"}]},
                    "/api/v1/chats/chat1": {"id": "chat1", "status": "idle"},
                    "/api/v1/chats/chat1/messages?limit=100": {"resources": [{"content": "saved"}], "hasMore": True, "nextBeforeSequence": 12},
                    "/api/v1/chats/chat1/events/history?after=0": {"resources": []},
                }
                if self.path == "/redirect":
                    self.send_response(302)
                    self.send_header("Location", "http://example.invalid/secret")
                    self.end_headers()
                    return
                self.reply(responses.get(self.path, {}), status=200 if self.path in responses else 404)

            def reply(self, value, status=200, cookie=False):
                encoded = json.dumps(value).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                if cookie:
                    self.send_header("Set-Cookie", "session=private-cookie; HttpOnly; Path=/")
                self.end_headers()
                self.wfile.write(encoded)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.url = "http://127.0.0.1:" + str(self.server.server_port)
        self.case = {"id": "WA01", "title": "虚构评测", "user_messages": ["建立虚构流程", "仅手动发送的第二轮"], "result": {"score": None}}
        self.defaults = {"runtimeId": "example-runtime", "permissionLevel": "default"}

    def client(self):
        return self.module.Client(self.url, self.url)

    def start(self):
        return self.module.start(self.client(), self.root, self.case, self.defaults)

    def test_start_sends_only_first_message_and_replays_same_state(self):
        state_path = self.start()
        self.start()
        self.assertEqual(len(self.creates), 1)
        self.assertEqual(len(self.sends), 1)
        sent = next(iter(self.sends))
        bodies = [body for method, path, body, _ in self.requests if path.endswith("/creation/messages")]
        self.assertEqual(len(bodies), 1)
        self.assertEqual(bodies[0]["content"], "建立虚构流程")
        self.assertEqual(bodies[0]["defaults"], self.defaults)
        self.assertEqual(bodies[0]["requestId"], sent)
        serialized = state_path.read_text()
        self.assertNotIn("private-csrf", serialized)
        self.assertNotIn("private-cookie", serialized)
        self.assertIsNone(json.loads(serialized)["score"])

    def test_unknown_post_outcome_reuses_persisted_request_ids(self):
        for suffix in ("", "/definition1/creation/messages"):
            with self.subTest(path=suffix):
                self.root = self.root / ("create" if not suffix else "message")
                self.drop_path = "/api/v1/workflows" + suffix
                with self.assertRaises(self.module.EvalError):
                    self.start()
                before = set(self.creates if not suffix else self.sends)
                self.start()
                self.assertEqual(set(self.creates if not suffix else self.sends), before)

    def test_changed_pending_payload_is_rejected_without_new_post(self):
        self.drop_path = "/api/v1/workflows/definition1/creation/messages"
        with self.assertRaises(self.module.EvalError):
            self.start()
        count = len(self.requests)
        self.case["user_messages"][0] = "改变发送内容"
        with self.assertRaises(self.module.EvalError):
            self.start()
        self.assertEqual(len(self.requests), count)

    def test_snapshot_keeps_raw_evidence_and_window_limits_without_sending(self):
        state_path = self.start()
        before = len(self.sends)
        destination = self.module.snapshot(self.client(), state_path)
        evidence = json.loads((destination / "evidence.json").read_text())
        self.assertEqual(evidence["source"]["revisionId"], "revision1")
        self.assertTrue(evidence["chats"]["chat1"]["messages"]["hasMore"])
        self.assertEqual(evidence["scope"]["messages_limit"], 100)
        self.assertIsNone(evidence["score"])
        self.assertEqual(len(self.sends), before)
        self.assertEqual(len([r for r in self.requests if r[0] == "GET"]), 6)

    def test_followup_requires_explicit_key_and_is_idempotent(self):
        state_path = self.start()
        self.module.followup(self.client(), state_path, "revision-1", "明确授权的新消息", self.defaults)
        self.module.followup(self.client(), state_path, "revision-1", "明确授权的新消息", self.defaults)
        self.assertEqual(len(self.sends), 2)
        bodies = [r[2] for r in self.requests if r[1].endswith("/creation/messages")]
        self.assertEqual(bodies[-1]["sessionId"], "session1")
        self.assertEqual(bodies[-1]["content"], "明确授权的新消息")

    def test_holdout_requires_explicit_opt_in(self):
        self.case["holdout"] = True
        with self.assertRaises(self.module.EvalError):
            self.start()
        self.assertEqual(self.requests, [])

    def test_remote_hosts_and_redirects_are_rejected(self):
        for url in ("https://example.invalid", "http://127.0.0.1@example.invalid", "http://0.0.0.0", "http://127.0.0.1/path"):
            with self.subTest(url=url), self.assertRaises(self.module.EvalError):
                self.module.Client(url, self.url)
        with self.assertRaises(self.module.EvalError):
            self.client().request("GET", "/redirect")

    def test_cli_accepts_explicit_prompt_file_without_case_and_prints_no_credentials(self):
        defaults_file = self.root / "defaults.json"
        defaults_file.write_text(json.dumps(self.defaults))
        prompt_file = self.root / "prompt.txt"
        prompt_file.write_text("明确指定的一条输入", encoding="utf-8")
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = self.module.main(["--base-url", self.url, "--origin", self.url,
                                     "--defaults-file", str(defaults_file), "--output-dir", str(self.root),
                                     "start", "--case-id", "WA99", "--prompt-file", str(prompt_file)])
        self.assertEqual(code, 0, stderr.getvalue())
        self.assertIsNone(json.loads(stdout.getvalue())["score"])
        self.assertNotIn("private-csrf", stdout.getvalue() + stderr.getvalue())
        self.assertNotIn("private-cookie", stdout.getvalue() + stderr.getvalue())
        bodies = [r[2] for r in self.requests if r[1].endswith("/creation/messages")]
        self.assertEqual([body["content"] for body in bodies], ["明确指定的一条输入"])


if __name__ == "__main__":
    unittest.main()
