#!/usr/bin/env python3
"""通过本机 Core 收集工作流创作评测证据，不执行运行、审批或自动评分。

例：
  python3 scripts/workflow-authoring-eval.py --base-url http://127.0.0.1:8432 \
    --origin http://127.0.0.1:1422 --defaults-file /tmp/defaults.json \
    --output-dir /tmp/workflow-eval start --case-file /tmp/cases.json --case-id WA01
  # 再以相同全局连接参数调用 snapshot --state /tmp/workflow-eval/WA01/state.json。
  # followup 另需 --message-key 唯一标识和 --prompt-file 显式消息文件。

start 的重复调用复用已落盘的 requestId；未知网络结果不创建新身份。
snapshot 每个端点只读一次，消息为最新 100 条窗口，事件为 after=0 的一页。
运行产物必须放在仓库之外。Cookie 与 CSRF 只保留在当前进程内存。
"""

import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import http.client
import http.cookiejar
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid


class EvalError(Exception):
    """可安全显示的评测驱动错误，不携带认证响应或请求头。"""


def loopback_url(value):
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("http", "https") or parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise EvalError("连接地址必须是无凭据、无路径的本机 HTTP(S) origin")
    try:
        local = parsed.hostname == "localhost" or ipaddress.ip_address(parsed.hostname).is_loopback
        parsed.port  # 同时拒绝非法端口。
    except (ValueError, TypeError):
        local = False
    if not local:
        raise EvalError("仅允许 loopback 服务，拒绝远端地址")
    return value.rstrip("/")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, new_url):
        raise EvalError("拒绝 HTTP 重定向；未向新地址发送认证信息")


class Client:
    """仅连接 loopback，使用 Web session cookie 与 CSRF 的单次请求客户端。"""

    def __init__(self, base_url, origin):
        self.base_url = loopback_url(base_url)
        self.origin = loopback_url(origin)
        self.csrf = None
        self.opener = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
            NoRedirect(),
        )

    def request(self, method, path, body=None):
        if not path.startswith("/") or path.startswith("//"):
            raise EvalError("请求路径必须属于当前本机服务")
        if self.csrf is None and path != "/api/web/session":
            session = self.request("POST", "/api/web/session", {})
            self.csrf = session.get("csrfToken")
            if not isinstance(self.csrf, str) or not self.csrf:
                raise EvalError("本机会话未返回 CSRF；请检查 origin 和本机 Web 服务")
        headers = {"Origin": self.origin, "Accept": "application/json"}
        if self.csrf:
            headers["X-Ally-CSRF"] = self.csrf
        encoded = None
        if body is not None:
            encoded = json.dumps(body, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.base_url + path, data=encoded, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise EvalError(f"HTTP {error.code}: {method} {path}；保留原 requestId 后可显式重试") from None
        except (urllib.error.URLError, http.client.HTTPException, TimeoutError, OSError, ValueError):
            raise EvalError(f"请求结果未知: {method} {path}；未自动重试，原 requestId 已保留") from None


def read_json(path):
    with Path(path).open(encoding="utf-8") as stream:
        return json.load(stream)


def artifact_path(path):
    path = Path(path).expanduser().resolve()
    repository = Path(__file__).resolve().parents[1]
    if path == repository or repository in path.parents:
        raise EvalError("私有评测证据必须写入仓库之外的 output-dir")
    return path


def write_json(path, value):
    path = artifact_path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name("." + path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


@contextmanager
def state_lock(path):
    path = artifact_path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with os.fdopen(os.open(path.with_suffix(".lock"), os.O_RDWR | os.O_CREAT, 0o600), "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def identity(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", value):
        raise EvalError("case-id/message-key 必须是 1–64 位字母数字和 ._-，首位为字母数字")
    return value


def digest(value):
    data = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def operation(client, state_path, state, key, path, payload):
    saved = state["operations"].get(key)
    if saved is None:
        saved = {"requestId": "eval-" + uuid.uuid4().hex, "path": path, "payload": payload}
        state["operations"][key] = saved
        write_json(state_path, state)  # 发出任何业务 POST 前，持久化同一身份和负载。
    if saved["path"] != path or saved["payload"] != payload:
        raise EvalError("同一操作的负载已变化；拒绝以原身份发送或自动创建另一实例")
    if "response" not in saved:
        saved["response"] = client.request("POST", path, {**payload, "requestId": saved["requestId"]})
        write_json(state_path, state)
    return saved["response"]


def check_binding(client, state):
    if state["base_url"] != client.base_url or state["origin"] != client.origin:
        raise EvalError("state 绑定的本机地址或 origin 不匹配")


def start(client, output_dir, case, defaults, prompt=None, allow_holdout=False):
    if case.get("holdout") and not allow_holdout:
        raise EvalError("holdout 不会自动运行；需要单独显式 --allow-holdout")
    case_id = identity(case.get("id"))
    messages = case.get("user_messages", [])
    content = prompt if prompt is not None else (messages[0] if messages else None)
    if not isinstance(content, str) or not content.strip():
        raise EvalError("需要非空 user_messages[0] 或显式 prompt-file")
    signature = digest({"case_id": case_id, "content": content, "defaults": defaults})
    state_path = artifact_path(output_dir) / case_id / "state.json"
    with state_lock(state_path):
        if state_path.exists():
            state = read_json(state_path)
            check_binding(client, state)
            if state["start_digest"] != signature:
                raise EvalError("该 case 已有不同启动负载，拒绝覆盖；请使用另一 output-dir")
        else:
            state = {"version": 1, "case_id": case_id, "base_url": client.base_url, "origin": client.origin,
                     "start_digest": signature, "operations": {}, "score": None}
            write_json(state_path, state)
            write_json(state_path.with_name("case.json"), case)
        definition = operation(client, state_path, state, "create", "/api/v1/workflows", {})
        definition_id = identity(definition.get("id"))
        state["definition_id"] = definition_id
        write_json(state_path, state)
        session = operation(client, state_path, state, "initial_message", f"/api/v1/workflows/{definition_id}/creation/messages", {"content": content, "defaults": defaults})
        state["session_id"] = session.get("id")
        state["chat_id"] = session.get("chatId")
        write_json(state_path, state)
    return state_path


def followup(client, state_path, message_key, prompt, defaults):
    message_key = identity(message_key)
    if not isinstance(prompt, str) or not prompt.strip():
        raise EvalError("followup 需要显式非空 prompt-file")
    state_path = artifact_path(state_path)
    with state_lock(state_path):
        state = read_json(state_path)
        check_binding(client, state)
        if not state.get("session_id"):
            raise EvalError("初始消息尚未确认接纳；先以原 start 请求恢复")
        definition_id = identity(state.get("definition_id"))
        operation(client, state_path, state, "followup:" + message_key,
                  f"/api/v1/workflows/{definition_id}/creation/messages",
                  {"sessionId": state["session_id"], "content": prompt, "defaults": defaults})
    return state_path


def snapshot(client, state_path):
    state_path = artifact_path(state_path)
    state = read_json(state_path)
    check_binding(client, state)
    definition_id = identity(state.get("definition_id"))
    prefix = "/api/v1/workflows/" + definition_id
    evidence = {"captured_at": datetime.now(timezone.utc).isoformat(), "case_id": state["case_id"], "score": None,
                "scope": {"polls": 1, "messages_limit": 100, "events_after": 0,
                          "coverage": "每个端点一页；保留 hasMore/游标，不能据此声称完整覆盖或通过"}, "chats": {}}
    evidence["definition"] = client.request("GET", prefix)
    evidence["source"] = client.request("GET", prefix + "/source")
    evidence["events"] = client.request("GET", prefix + "/events/history?after=0")
    chat_ids = {session.get("chatId") for session in evidence["definition"].get("creationSessions", [])}
    chat_ids.add(state.get("chat_id"))
    for chat_id in sorted(value for value in chat_ids if value):
        chat_id = identity(chat_id)
        chat_prefix = "/api/v1/chats/" + chat_id
        evidence["chats"][chat_id] = {"chat": client.request("GET", chat_prefix),
                                     "messages": client.request("GET", chat_prefix + "/messages?limit=100"),
                                     "events": client.request("GET", chat_prefix + "/events/history?after=0")}
    destination = state_path.parent / "snapshots" / (datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S") + "-" + uuid.uuid4().hex[:8])
    write_json(destination / "evidence.json", evidence)
    return destination


def load_case(path, case_id):
    document = read_json(path)
    cases = document.get("cases", [document])
    matches = [case for case in cases if case.get("id") == case_id]
    if len(matches) != 1:
        raise EvalError("case-id 必须唯一匹配 case-file 中的一例")
    return matches[0]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--origin", required=True)
    parser.add_argument("--defaults-file")
    parser.add_argument("--output-dir")
    commands = parser.add_subparsers(dest="command", required=True)
    starting = commands.add_parser("start", help="只创建定义并发送首条消息；相同参数可安全重放")
    starting.add_argument("--case-file")
    starting.add_argument("--case-id", required=True)
    starting.add_argument("--prompt-file")
    starting.add_argument("--allow-holdout", action="store_true")
    for name in ("snapshot", "poll", "followup"):
        command = commands.add_parser(name, help="仅获取一次快照" if name != "followup" else "显式发送一条续发消息")
        command.add_argument("--state", required=True)
        if name == "followup":
            command.add_argument("--message-key", required=True)
            command.add_argument("--prompt-file", required=True)
    args = parser.parse_args(argv)
    try:
        client = Client(args.base_url, args.origin)
        if args.command in ("start", "followup"):
            if not args.defaults_file:
                raise EvalError("start/followup 必须显式提供 defaults-file")
            defaults = read_json(args.defaults_file)
            if "defaults" in defaults:
                defaults = defaults["defaults"]
            if not isinstance(defaults, dict):
                raise EvalError("defaults-file 必须包含执行配置 JSON 对象")
            prompt = Path(args.prompt_file).read_text(encoding="utf-8") if args.prompt_file else None
            if args.command == "start":
                if not args.output_dir:
                    raise EvalError("start 必须指定仓库之外的 output-dir")
                if not args.case_file and prompt is None:
                    raise EvalError("start 需要 case-file 或显式 prompt-file")
                case = load_case(args.case_file, args.case_id) if args.case_file else {"id": args.case_id, "user_messages": []}
                result = start(client, args.output_dir, case, defaults, prompt, args.allow_holdout)
            else:
                result = followup(client, args.state, args.message_key, prompt, defaults)
        else:
            result = snapshot(client, args.state)
        print(json.dumps({"command": args.command, "artifact": str(result), "score": None}, ensure_ascii=False))
        return 0
    except (EvalError, OSError, ValueError, KeyError, TypeError):
        # 不打印原始异常文本、认证响应或 defaults，以免外部响应携带凭据。
        error = sys.exc_info()[1]
        message = str(error) if isinstance(error, EvalError) else "本地输入或状态无效；请检查文件与参数，原请求状态未重置"
        print(message, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
