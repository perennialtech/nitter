"""Exercise Nitter's HTTP requests through local HTTP and HTTPS CONNECT proxies."""

from pathlib import Path
import base64
import socket
import ssl
import subprocess
import tempfile
import threading
import unittest


class HttpProxyTests(unittest.TestCase):
    path = "/graphql/test?variables=%7B%22screen_name%22%3A%22mobile_test%22%7D&features=%7B%7D"

    @classmethod
    def setUpClass(cls):
        temporary = tempfile.TemporaryDirectory(prefix="nitter-http-proxy-")
        cls.addClassCleanup(temporary.cleanup)
        cls.directory = Path(temporary.name)
        cls.client = cls.directory / "http_proxy_client"
        root = Path(__file__).resolve().parent.parent
        subprocess.run(
            ["nim", "c", "--hints:off", "--warnings:off", f"--out:{cls.client}",
             str(root / "tests/http_proxy_client.nim")],
            cwd=root, check=True, timeout=120,
        )
        cls.certificate = cls.directory / "certificate.pem"
        cls.key = cls.directory / "key.pem"
        subprocess.run(
            ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
             "-keyout", str(cls.key), "-out", str(cls.certificate),
             "-subj", "/CN=origin.test", "-addext", "subjectAltName=DNS:origin.test",
             "-days", "1"],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            check=True, timeout=30,
        )

    @staticmethod
    def read_headers(connection):
        data = b""
        while b"\r\n\r\n" not in data:
            chunk = connection.recv(4096)
            if not chunk:
                raise EOFError("Client closed the proxy connection before sending headers")
            data += chunk
            if len(data) > 65536:
                raise ValueError("Proxy test request headers exceed 64 KiB")
        lines = data.decode().split("\r\n")
        headers = {}
        for line in lines[1:]:
            if line:
                key, value = line.split(":", 1)
                headers[key.lower()] = value.strip()
        return lines[0], headers

    def proxy_requests(self, tls, url_auth="", auth=""):
        observed, errors = [], []
        listener = socket.socket()
        self.addCleanup(listener.close)
        listener.bind(("127.0.0.1", 0))
        listener.listen(2)
        listener.settimeout(20)
        port = listener.getsockname()[1]

        def serve():
            try:
                for _ in range(2):
                    connection, _ = listener.accept()
                    connection.settimeout(10)
                    with connection:
                        if tls:
                            observed.append(self.read_headers(connection))
                            connection.sendall(
                                b"HTTP/1.1 200 Connection established\r\nContent-Length: 0\r\n\r\n"
                            )
                            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
                            context.load_cert_chain(self.certificate, self.key)
                            connection = context.wrap_socket(connection, server_side=True)
                        with connection:
                            observed.append(self.read_headers(connection))
                            connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
                            self.assertEqual(connection.recv(1), b"", "client retained proxy socket")
            except Exception as error:
                errors.append(error)

        server = threading.Thread(target=serve, daemon=True)
        server.start()
        scheme = "https" if tls else "http"
        authority = f"{url_auth}@" if url_auth else ""
        try:
            result = subprocess.run(
                [str(self.client), f"http://{authority}127.0.0.1:{port}",
                 f"{scheme}://origin.test{self.path}", str(self.certificate), auth],
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                text=True, timeout=30,
            )
        finally:
            server.join(timeout=15)
        self.assertFalse(server.is_alive(), "Proxy server did not finish")
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertFalse(errors, repr(errors))
        return observed

    def check_requests(self, requests, target):
        self.assertEqual(len(requests), 2)
        for index, (line, headers) in enumerate(requests):
            self.assertEqual(line, f"GET {target} HTTP/1.1")
            self.assertEqual(headers["host"], "origin.test")
            self.assertEqual(headers["x-nitter-proxy-test"], str(index))

    def test_http_proxy_uses_absolute_form(self):
        self.check_requests(self.proxy_requests(tls=False), "http://origin.test" + self.path)

    def test_https_connect_uses_origin_form(self):
        requests = self.proxy_requests(tls=True)
        self.assertEqual(requests[0][0], "CONNECT origin.test:443 HTTP/1.1")
        self.assertEqual(requests[0][1]["host"], "origin.test:443")
        self.assertEqual(requests[2][0], "CONNECT origin.test:443 HTTP/1.1")
        self.check_requests([requests[1], requests[3]], self.path)

    def check_auth(self, requests, credentials):
        self.check_requests(requests, "http://origin.test" + self.path)
        expected = "Basic " + base64.b64encode(credentials.encode()).decode()
        for _, headers in requests:
            self.assertEqual(headers["proxy-authorization"], expected)

    def test_http_proxy_accepts_url_auth(self):
        self.check_auth(self.proxy_requests(tls=False, url_auth="user:password"), "user:password")

    def test_http_proxy_accepts_separate_auth(self):
        self.check_auth(self.proxy_requests(tls=False, auth="user:password"), "user:password")

    def test_http_proxy_separate_auth_overrides_url_auth(self):
        requests = self.proxy_requests(tls=False, url_auth="old:password", auth="new:password")
        self.check_auth(requests, "new:password")

    def test_http_proxy_auth_allows_colons_in_password(self):
        self.check_auth(self.proxy_requests(tls=False, auth="user:pass:word"), "user:pass:word")

    def test_http_proxy_rejects_auth_without_separator(self):
        result = subprocess.run(
            [str(self.client), "http://127.0.0.1:1", "http://origin.test/",
             str(self.certificate), "invalid"],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, timeout=10,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("proxyAuth must contain username:password", result.stdout)

    def test_rejected_connect_closes_proxy_socket(self):
        listener = socket.socket()
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        self.addCleanup(listener.close)
        port = listener.getsockname()[1]
        observed = []

        def serve():
            connection, _ = listener.accept()
            with connection:
                connection.settimeout(5)
                observed.append(self.read_headers(connection)[0])
                connection.sendall(b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
                observed.append(connection.recv(1))

        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        result = subprocess.run(
            [str(self.client), f"http://127.0.0.1:{port}",
             "https://origin.test/path", str(self.certificate), "", "reject-connect"],
            capture_output=True, text=True, timeout=15,
        )
        thread.join(timeout=8)
        self.assertFalse(thread.is_alive(), "Failed CONNECT left a proxy socket open")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(observed, ["CONNECT origin.test:443 HTTP/1.1", b""])


if __name__ == "__main__":
    unittest.main()
