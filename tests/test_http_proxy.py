"""Exercise Nitter's HTTP pool through local HTTP and HTTPS CONNECT proxies."""

from pathlib import Path
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

    def proxy_requests(self, tls):
        observed, errors = [], []
        listener = socket.socket()
        self.addCleanup(listener.close)
        listener.bind(("127.0.0.1", 0))
        listener.listen(1)
        listener.settimeout(20)
        port = listener.getsockname()[1]

        def serve():
            try:
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
                        for _ in range(2):
                            observed.append(self.read_headers(connection))
                            connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
            except Exception as error:
                errors.append(error)

        server = threading.Thread(target=serve, daemon=True)
        server.start()
        scheme = "https" if tls else "http"
        try:
            result = subprocess.run(
                [str(self.client), f"http://127.0.0.1:{port}",
                 f"{scheme}://origin.test{self.path}", str(self.certificate)],
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
        self.check_requests(requests[1:], self.path)


if __name__ == "__main__":
    unittest.main()
