"""Exercise raw media responses against a local Twitter-host fixture."""

import base64
from concurrent.futures import ThreadPoolExecutor
import http.client
import os
import socket
import struct
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PAYLOAD = b"media proxy response" * 4096
STREAM_BLOCK = b"x" * 65536


class MediaHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    release_stalls = threading.Event()
    pending_header_requests = 0
    pending_header_lock = threading.Lock()
    release_headers = threading.Event()

    def do_GET(self):
        if self.path == "/pending-headers":
            with self.pending_header_lock:
                MediaHandler.pending_header_requests += 1
            self.release_headers.wait(10)
            return
        if self.path == "/delayed-headers":
            # Headers are slow, but the body is healthy and arrives less than
            # the stream idle deadline after the headers were received.
            time.sleep(2.4)
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", "18")
            self.end_headers()
            time.sleep(3.0)
            try:
                self.wfile.write(b"valid delayed body")
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            return

        if self.path == "/chunked":
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            try:
                for _ in range(900):
                    self.wfile.write(b"1\r\nx\r\n")
                    self.wfile.flush()
                    time.sleep(0.002)
                self.wfile.write(b"0\r\n\r\n")
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            return

        if self.path in ("/slow-big", "/fast-big"):
            self.send_response(200)
            self.send_header("Content-Type", "video/mp4")
            self.send_header("Content-Length", str(len(STREAM_BLOCK) * 1024))
            self.end_headers()
            try:
                for _ in range(1024):
                    self.wfile.write(STREAM_BLOCK)
                    self.wfile.flush()
                    if self.path == "/slow-big":
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
        MediaHandler.release_headers.set()
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

    def test_abandoned_media_does_not_hold_upstream_sockets(self):
        before = len(os.listdir(self.fd_path))
        media_url = f"http://pbs.twimg.com:{self.port}/fast-big"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()

        def abandon(_):
            host, port = self.base.rsplit(":", 1)
            with socket.create_connection((host, int(port)), timeout=10) as sock:
                request = (f"GET /pic/enc/{encoded} HTTP/1.1\r\n"
                           f"Host: {self.base}\r\nConnection: close\r\n\r\n")
                sock.sendall(request.encode())
                headers = b""
                while b"\r\n\r\n" not in headers:
                    chunk = sock.recv(4096)
                    self.assertTrue(chunk, "media response ended before headers")
                    headers += chunk
                self.assertIn(b" 200 ", headers.split(b"\r\n", 1)[0])
                # Abort with a real TCP reset while unread media remains.
                sock.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER,
                                struct.pack("ii", 1, 0))

        with ThreadPoolExecutor(max_workers=4) as pool:
            list(pool.map(abandon, range(36)))

        # Previously the upstream sockets survived until the five-second
        # idle watchdog fired, accumulating during bursts of client aborts.
        time.sleep(1)
        after = len(os.listdir(self.fd_path))
        self.assertLessEqual(after - before, 4, "abandoned streams retained sockets")

    def test_half_closed_client_can_still_receive_media(self):
        media_url = f"http://pbs.twimg.com:{self.port}/length"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        sock = socket.create_connection(("127.0.0.1", int(self.base.rsplit(":", 1)[1])), timeout=8)
        try:
            request = (f"GET /pic/enc/{encoded} HTTP/1.1\r\n"
                       f"Host: {self.base}\r\nConnection: close\r\n\r\n")
            sock.sendall(request.encode())
            sock.shutdown(socket.SHUT_WR)
            response = http.client.HTTPResponse(sock)
            response.begin()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.read(), PAYLOAD)
        finally:
            sock.close()

    def test_disconnected_client_does_not_wait_for_upstream_headers(self):
        before = len(os.listdir(self.fd_path))
        media_url = f"http://pbs.twimg.com:{self.port}/pending-headers"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        count = 12
        with MediaHandler.pending_header_lock:
            MediaHandler.pending_header_requests = 0

        clients = []
        try:
            for _ in range(count):
                conn = http.client.HTTPConnection(self.base, timeout=5)
                conn.request("GET", f"/pic/enc/{encoded}")
                clients.append(conn)

            deadline = time.monotonic() + 3
            while time.monotonic() < deadline:
                with MediaHandler.pending_header_lock:
                    seen = MediaHandler.pending_header_requests
                if seen >= count:
                    break
                time.sleep(0.02)
            self.assertEqual(seen, count, "not all upstream headers were pending")
        finally:
            for conn in clients:
                if conn.sock:
                    # An RST is distinguishable from a valid half-close.
                    conn.sock.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER,
                                         struct.pack("ii", 1, 0))
                conn.close()

        try:
            time.sleep(1)
            after = len(os.listdir(self.fd_path))
            self.assertLessEqual(after - before, 4,
                                 "abandoned header fetches retained socket pairs")
            # A disconnected request must not retry after the original
            # five-second header timeout.
            time.sleep(5)
            with MediaHandler.pending_header_lock:
                self.assertEqual(MediaHandler.pending_header_requests, count,
                                 "disconnected requests retried upstream")
        finally:
            MediaHandler.release_headers.set()

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

    def assert_upstream_media_is_bounded(self, path):
        pid = os.environ["NITTER_TEST_PID"]

        def rss_kib():
            with open(f"/proc/{pid}/status", encoding="ascii") as status:
                return int(next(line for line in status if line.startswith("VmRSS:")).split()[1])

        stop = threading.Event()
        started = threading.Barrier(4)
        errors = []

        def slow_reader():
            media_url = f"http://pbs.twimg.com:{self.port}/{path}"
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

    def test_slow_readers_do_not_buffer_entire_upstream_media(self):
        self.assert_upstream_media_is_bounded("slow-big")

    def test_fast_upstream_does_not_fill_the_buffer_before_backpressure(self):
        self.assert_upstream_media_is_bounded("fast-big")

    def test_fast_upstream_transfers_complete_body(self):
        media_url = f"http://pbs.twimg.com:{self.port}/fast-big"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        conn = http.client.HTTPConnection(self.base, timeout=20)
        try:
            conn.request("GET", f"/pic/enc/{encoded}")
            response = conn.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.getheader("Content-Length"), str(len(STREAM_BLOCK) * 1024))
            total = 0
            while chunk := response.read(len(STREAM_BLOCK)):
                self.assertEqual(chunk, STREAM_BLOCK)
                total += len(chunk)
            self.assertEqual(total, len(STREAM_BLOCK) * 1024)
        finally:
            conn.close()

    def test_chunked_media_does_not_stall_on_framing_bytes(self):
        media_url = f"http://pbs.twimg.com:{self.port}/chunked"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        conn = http.client.HTTPConnection(self.base, timeout=10)
        try:
            conn.request("GET", f"/pic/enc/{encoded}")
            response = conn.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.read(), b"x" * 900)
        finally:
            conn.close()

    def test_slow_headers_do_not_consume_the_body_idle_timeout(self):
        media_url = f"http://pbs.twimg.com:{self.port}/delayed-headers"
        encoded = base64.urlsafe_b64encode(media_url.encode()).decode()
        conn = http.client.HTTPConnection(self.base, timeout=12)
        try:
            conn.request("GET", f"/pic/enc/{encoded}")
            response = conn.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.read(), b"valid delayed body")
        finally:
            conn.close()


if __name__ == "__main__":
    unittest.main()
