"""Exercise raw media responses against a local Twitter-host fixture."""

import base64
import http.client
import os
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PAYLOAD = b"media proxy response" * 4096
STREAM_BLOCK = b"x" * 65536


class MediaHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    release_stalls = threading.Event()

    def do_GET(self):
        if self.path == "/slow-big":
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(STREAM_BLOCK) * 1024))
            self.end_headers()
            try:
                for _ in range(1024):
                    self.wfile.write(STREAM_BLOCK)
                    self.wfile.flush()
                    time.sleep(0.006)
            except (BrokenPipeError, ConnectionResetError):
                pass
            return

        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        if self.path in ("/length", "/stall"):
            self.send_header("Content-Length", str(len(PAYLOAD)))
        else:
            self.send_header("Connection", "close")
            self.close_connection = True
        self.end_headers()
        if self.path == "/stall":
            self.wfile.write(b"partial")
            self.wfile.flush()
            self.release_stalls.wait(20)
            self.close_connection = True
            return
        self.wfile.write(PAYLOAD)

    def log_message(self, *args):
        pass


@unittest.skipUnless(
    os.environ.get("NITTER_TEST_URL") and os.environ.get("NITTER_TEST_PID"),
    "Requires a running Nitter instance and its PID",
)
class MediaSocketCleanupTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), MediaHandler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.fd_path = f"/proc/{os.environ['NITTER_TEST_PID']}/fd"
        cls.base = os.environ["NITTER_TEST_URL"].removeprefix("http://")
        cls.port = cls.server.server_address[1]

    @classmethod
    def tearDownClass(cls):
        MediaHandler.release_stalls.set()
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def test_streams_close_and_release_downstream_sockets(self):
        before = len(os.listdir(self.fd_path))
        for resource in ("no-length", "length"):
            media_url = f"http://pbs.twimg.com:{self.port}/{resource}"
            encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
            for _ in range(30):
                conn = http.client.HTTPConnection(self.base, timeout=5)
                try:
                    conn.request("GET", f"/pic/enc/{encoded}")
                    response = conn.getresponse()
                    self.assertEqual(response.status, 200)
                    self.assertEqual(response.getheader("Connection"), "close")
                    self.assertEqual(response.getheader("Content-Length") is not None,
                                     resource == "length")
                    self.assertEqual(response.read(), PAYLOAD)
                finally:
                    conn.close()

        time.sleep(0.25)
        after = len(os.listdir(self.fd_path))
        self.assertLessEqual(after - before, 4, "raw media sockets are not released")

    def test_stalled_upstream_body_closes_stream(self):
        before = len(os.listdir(self.fd_path))
        media_url = f"http://pbs.twimg.com:{self.port}/stall"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        # Repeated failures distinguish genuine cleanup from the old
        # four-FD allowance accidentally accepting one leaked socket pair.
        for _ in range(6):
            conn = http.client.HTTPConnection(self.base, timeout=9)
            try:
                conn.request("GET", f"/pic/enc/{encoded}")
                response = conn.getresponse()
                self.assertEqual(response.status, 200)
                with self.assertRaises(http.client.IncompleteRead):
                    response.read()
            finally:
                conn.close()
        time.sleep(0.25)
        after = len(os.listdir(self.fd_path))
        self.assertLessEqual(after - before, 2, "stalled media streams retained sockets")
        # A detached watchdog failure must not stop the server event loop.
        conn = http.client.HTTPConnection(self.base, timeout=5)
        try:
            conn.request("GET", "/")
            response = conn.getresponse()
            self.assertEqual(response.status, 200)
            self.assertTrue(response.read())
        finally:
            conn.close()

    def test_slow_readers_do_not_buffer_entire_upstream_media(self):
        pid = os.environ["NITTER_TEST_PID"]

        def rss_kib():
            with open(f"/proc/{pid}/status", encoding="ascii") as status:
                return int(next(line for line in status if line.startswith("VmRSS:")).split()[1])

        stop = threading.Event()
        started = threading.Barrier(4)
        errors = []

        def slow_reader():
            media_url = f"http://pbs.twimg.com:{self.port}/slow-big"
            encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
            conn = http.client.HTTPConnection(self.base, timeout=15)
            try:
                conn.request("GET", f"/pic/enc/{encoded}")
                response = conn.getresponse()
                if response.status != 200:
                    errors.append(f"media request returned {response.status}")
                started.wait(timeout=10)
                while not stop.is_set():
                    if not response.read(65536):
                        break
                    time.sleep(0.08)
            except Exception as exc:
                errors.append(str(exc))
            finally:
                conn.close()

        before = rss_kib()
        readers = [threading.Thread(target=slow_reader) for _ in range(3)]
        for reader in readers:
            reader.start()
        try:
            started.wait(timeout=10)
            # Three 64 MiB upstreams fill FutureStream under the old code,
            # whereas forwarding-aware backpressure keeps the queue small.
            peak = before
            for _ in range(8):
                time.sleep(1)
                peak = max(peak, rss_kib())
            self.assertLess(peak - before, 80 * 1024,
                            f"media queues grew RSS by {(peak - before) / 1024:.1f} MiB")
        finally:
            stop.set()
            for reader in readers:
                reader.join(timeout=10)
        self.assertFalse(errors, errors)


if __name__ == "__main__":
    unittest.main()
