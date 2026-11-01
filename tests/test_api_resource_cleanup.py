"""Integration regression for abandoned requests to a stalled Twitter API.

Runs against a local Nitter binary, dummy credentials and an isolated HTTP
fixture. No real Twitter credentials or internet connectivity are required.
"""

import json
import os
import re
import socket
import struct
import subprocess
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen


ROOT = Path(__file__).resolve().parent.parent


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def replace_option(config, key, value):
    result, count = re.subn(
        rf"(?m)^{re.escape(key)}\s*=.*$", f"{key} = {value}", config
    )
    assert count == 1, f"Missing Nitter configuration setting: {key}"
    return result


def fd_count(pid):
    return len(os.listdir(f"/proc/{pid}/fd"))


def wait_until(condition, timeout=5):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if condition():
            return True
        time.sleep(0.025)
    return condition()


class StalledApiHandler(BaseHTTPRequestHandler):
    requests = 0
    lock = threading.Lock()

    def do_GET(self):
        with self.lock:
            type(self).requests += 1
        # Longer than the Nitter test configuration's one-second deadline.
        time.sleep(8)

    def log_message(self, *_args):
        pass


class ApiResourceCleanupTests(unittest.TestCase):
    def test_abandoned_api_requests_release_sockets_and_redis_leases(self):
        with tempfile.TemporaryDirectory(prefix="nitter-api-cleanup-") as temp:
            temp = Path(temp)
            StalledApiHandler.requests = 0
            fixture = ThreadingHTTPServer(("127.0.0.1", 0), StalledApiHandler)
            fixture.daemon_threads = True
            thread = threading.Thread(target=fixture.serve_forever, daemon=True)
            thread.start()

            port = free_port()
            config = (ROOT / "nitter.example.conf").read_text()
            settings = {
                "port": port,
                "redisHost": '"127.0.0.1"',
                "redisPort": int(os.getenv("NITTER_TEST_REDIS_PORT", "6379")),
                "redisConnections": 1,
                "redisMaxConnections": 2,
                "apiProxy": f'"http://127.0.0.1:{fixture.server_port}"',
                "maxConcurrentReqs": 64,
                "maxRetries": 1,
                "apiTimeoutMs": 1000,
            }
            for key, value in settings.items():
                config = replace_option(config, key, str(value))
            config_path = temp / "nitter.conf"
            config_path.write_text(config)
            sessions_path = temp / "sessions.jsonl"
            sessions_path.write_text(
                json.dumps({
                    "kind": "oauth", "username": "fixture",
                    "oauth_token": "12345-fixture",
                    "oauth_token_secret": "test-only",
                }) + "\n"
            )

            env = os.environ.copy()
            env["NITTER_CONF_FILE"] = str(config_path)
            env["NITTER_SESSIONS_FILE"] = str(sessions_path)
            log_path = temp / "nitter.log"
            clients = []
            with log_path.open("w+") as log:
                proc = subprocess.Popen(
                    [str(ROOT / "nitter")], cwd=ROOT, env=env,
                    stdout=log, stderr=subprocess.STDOUT,
                )
                try:
                    def ready():
                        try:
                            with urlopen(f"http://127.0.0.1:{port}/", timeout=1) as res:
                                return res.status == 200
                        except OSError:
                            return False

                    self.assertTrue(wait_until(ready, 10), "Nitter did not start")
                    before = fd_count(proc.pid)
                    count = 24
                    for i in range(count):
                        client = socket.create_connection(("127.0.0.1", port), 3)
                        client.sendall(
                            (f"GET /api_resource_test_{port}_{i} HTTP/1.1\r\n"
                             "Host: localhost\r\nConnection: close\r\n\r\n").encode()
                        )
                        clients.append(client)

                    self.assertTrue(
                        wait_until(lambda: StalledApiHandler.requests >= count, 5),
                        f"Only {StalledApiHandler.requests}/{count} requests reached the API",
                    )
                    # API sockets and downstream sockets are expected, but no
                    # extra Redis leases should be held while the API stalls.
                    peak = fd_count(proc.pid)
                    self.assertLessEqual(
                        peak, before + 2 * count + 10,
                        f"Redis connections grew with stalled lookups: {before} -> {peak}",
                    )

                    for client in clients:
                        client.setsockopt(
                            socket.SOL_SOCKET, socket.SO_LINGER,
                            struct.pack("ii", 1, 0),
                        )
                        client.close()
                    clients.clear()

                    self.assertTrue(
                        wait_until(lambda: fd_count(proc.pid) <= before + 6, 4),
                        f"Stalled API sockets were not released: {before} -> "
                        f"{fd_count(proc.pid)}",
                    )
                except Exception:
                    log.flush()
                    print("\nNitter diagnostic output:\n" + log_path.read_text()[-5000:])
                    raise
                finally:
                    for client in clients:
                        client.close()
                    proc.terminate()
                    try:
                        proc.wait(timeout=3)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()
                    fixture.shutdown()
                    fixture.server_close()


if __name__ == "__main__":
    unittest.main()
