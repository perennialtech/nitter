"""Exercise raw media responses against a local Twitter-host fixture."""

import base64
import http.client
import os
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PAYLOAD = b"media proxy response" * 4096


class MediaHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        if self.path == "/length":
            self.send_header("Content-Length", str(len(PAYLOAD)))
        else:
            self.send_header("Connection", "close")
            self.close_connection = True
        self.end_headers()
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


if __name__ == "__main__":
    unittest.main()
