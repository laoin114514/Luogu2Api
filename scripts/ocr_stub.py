#!/usr/bin/env python3
"""本地联调用的假 OCR 服务（永远返回固定验证码）。

用法：
    python3 scripts/ocr_stub.py            # 监听 127.0.0.1:9898，路径 /ocr

入参与返回都对齐现用的远程 OCR 服务，方便不依赖外网做本地联调：
    POST /ocr  {"image_base64": "<base64 JPEG>"}   （LUOGU_OCR_MODE=base64，默认）
    POST /ocr  原始 JPEG 字节                       （LUOGU_OCR_MODE=raw）
    -> {"sucess": true, "message": "识别成功", "data": {"text": "abcd"}}

它只是把"号池 → OCR → 登录"这条链路的管道跑通、观察日志，
**不能**让真实账号登录成功：固定返回的验证码必然被洛谷判为错误，
账号最终会走到 relogin_failed（这本身也是失败路径的验证手段）。
真实环境的自动重登必须接一个真的识别服务。
"""

import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 9898
CODE = "abcd"


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802 (标准库命名)
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        ctype = (self.headers.get("Content-Type") or "").lower()

        if "json" in ctype:
            try:
                payload = json.loads(raw or b"{}")
            except json.JSONDecodeError:
                payload = {}
            b64 = payload.get("image_base64") or ""
            detail = f"base64 {len(b64)} chars"
            if not b64:
                self._reply({"sucess": False, "message": "image_base64 不能为空", "data": {}})
                print(f"[ocr_stub] {self.path} <- {detail}, -> 参数错误", flush=True)
                return
        else:
            detail = f"raw {len(raw)} bytes"

        self._reply({"sucess": True, "message": "识别成功", "data": {"text": CODE}})
        print(f"[ocr_stub] {self.path} <- {detail}, -> {CODE}", flush=True)

    def _reply(self, payload):
        body = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):  # 静默默认访问日志
        pass


if __name__ == "__main__":
    print(f"[ocr_stub] listening on http://127.0.0.1:{PORT}/ocr", flush=True)
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
