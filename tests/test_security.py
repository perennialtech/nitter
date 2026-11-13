import subprocess
from parameterized import parameterized

BASE_URL = 'http://localhost:8080'


def curl_status(url):
    """Get HTTP status code using curl to avoid URL normalization by Python libs."""
    result = subprocess.run(
        ['curl', '-s', '-o', '/dev/null', '-w', '%{http_code}', url],
        capture_output=True, text=True, timeout=30
    )
    return int(result.stdout)


class TestMalformedPaths:
    """Test that malformed paths don't crash the server.

    Go keeps leading double slashes in request paths. Paths like //foo
    don't match a route and return 404. Repeated requests must leave the
    server responding.
    """

    @parameterized.expand([
        ('//lefty_rae', 404),
        ('//test', 404),
        ('//anyuser', 404),
    ])
    def test_double_slash_paths_return_404(self, path, expected_status):
        """Paths with leading double slashes should return 404 without crashing."""
        status = curl_status(f'{BASE_URL}{path}')
        assert status == expected_status, \
            f'Expected {expected_status} for {path}, got {status}'

    @parameterized.expand([
        ('/jack', 200),
        ('/about', 200),
        ('/', 200),
    ])
    def test_normal_paths_work(self, path, expected_status):
        """Normal paths should still work."""
        status = curl_status(f'{BASE_URL}{path}')
        assert status == expected_status, \
            f'Expected {expected_status} for {path}, got {status}'

    def test_server_survives_malformed_requests(self):
        """Server should handle malformed requests without crashing."""
        malformed_paths = ['//a', '//b', '//c', '//user', '//test']
        for path in malformed_paths:
            status = curl_status(f'{BASE_URL}{path}')
            assert status == 404, f'Expected 404 for {path}, got {status}'

        # Verify server is still responding after malformed requests
        status = curl_status(f'{BASE_URL}/')
        assert status == 200, 'Server should still be alive'
