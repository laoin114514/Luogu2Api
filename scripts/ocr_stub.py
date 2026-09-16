#!/usr/bin/env python3
"""本地联调用的假 OCR 服务（永远返回固定验证码）。

用法：
    python3 scripts/ocr_stub.py            # 监听 127.0.0.1:9898，路径 /ocr

它的作用只是把"号池 → OCR → 登录"这条链路的管道跑通、观察日志，
**不能**让真实账号登录成功：固定返回的验证码必然被洛谷判为错误，
账号最终会走到 relogin_failed（这本身也是失败路径的验证手段）。
真实环境的自动重登必须接一个真的识别服务。
"""

import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 9898


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):  # noqa: N802 (标准库命名)
        length = int(self.headers.get("Content-Length", 0))
        image = self.rfile.read(length)

        body = b"abcd"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

        print(f"[ocr_stub] {self.path} <- {len(image)} bytes, -> abcd", flush=True)

    def log_message(self, *args):  # 静默默认访问日志
        pass


if __name__ == "__main__":
    print(f"[ocr_stub] listening on http://127.0.0.1:{PORT}/ocr", flush=True)
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
