"""Private CPU embedding endpoint; neither text nor embeddings are logged."""
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
from fastembed import TextEmbedding

MODEL = os.environ.get("PCAS_EMBEDDING_MODEL", "BAAI/bge-small-zh-v1.5")
model = TextEmbedding(model_name=MODEL, cache_dir="/models", threads=int(os.environ.get("PCAS_EMBEDDING_THREADS", "2")))
lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def send_json(self, status, value):
        data = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self.send_json(200 if self.path == "/healthz" else 404, {"ready": self.path == "/healthz", "model": MODEL})

    def do_POST(self):
        if self.path != "/v1/embeddings":
            return self.send_json(404, {"error": "not_found"})
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 2 * 1024 * 1024:
                return self.send_json(413, {"error": "input_too_large"})
            body = json.loads(self.rfile.read(length))
            values = body["input"]
            if isinstance(values, str):
                values = [values]
            if body.get("model") != MODEL or not isinstance(values, list) or not 1 <= len(values) <= 128:
                raise ValueError()
            if any(not isinstance(t, str) or not t.strip() for t in values):
                raise ValueError()
            # Pool overlapping windows instead of silently losing long input tails.
            windows, ranges = [], []
            for text in values:
                begin = len(windows)
                for start in range(0, len(text), 320):
                    windows.append(text[start:start + 400])
                    if start + 400 >= len(text):
                        break
                ranges.append((begin, len(windows)))
            if len(windows) > 1024:
                return self.send_json(413, {"error": "too_many_windows"})
            with lock:
                embeddings = list(model.embed(windows, batch_size=16))
            data = []
            for index, (begin, end) in enumerate(ranges):
                vector = np.mean(embeddings[begin:end], axis=0)
                norm = np.linalg.norm(vector)
                if not norm:
                    raise ValueError()
                data.append({"object": "embedding", "index": index, "embedding": (vector / norm).tolist()})
            self.send_json(200, {"object": "list", "model": MODEL, "data": data})
        except (KeyError, TypeError, ValueError, json.JSONDecodeError):
            self.send_json(400, {"error": "invalid_input"})
        except Exception:
            self.send_json(503, {"error": "embedding_unavailable"})


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
