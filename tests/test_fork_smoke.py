"""Smoke tests for local pages and fork assets; no Twitter credentials needed."""

import os
import time
import unittest
from urllib.error import URLError
from urllib.parse import urlencode
from urllib.request import urlopen


@unittest.skipUnless(os.environ.get("NITTER_TEST_URL"), "Set NITTER_TEST_URL to a running Nitter instance")
class ForkSmokeTests(unittest.TestCase):
    themes = ("Frappe", "Latte", "Macchiato", "Mocha")

    @classmethod
    def setUpClass(cls):
        cls.base_url = os.environ["NITTER_TEST_URL"].rstrip("/")
        deadline = time.monotonic() + 30
        while True:
            try:
                with urlopen(cls.base_url + "/", timeout=2):
                    return
            except (URLError, TimeoutError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(0.25)

    def fetch(self, path):
        with urlopen(self.base_url + path, timeout=5) as response:
            self.assertEqual(response.status, 200)
            return response.headers.get_content_type(), response.read().decode()

    def test_homepage(self):
        content_type, body = self.fetch("/")
        self.assertEqual(content_type, "text/html")
        self.assertIn("Search", body)

    def test_about_links_to_fork(self):
        _, body = self.fetch("/about")
        self.assertIn("https://github.com/perennialtech/nitter/commit/", body)

    def test_theme_picker_contains_all_flavours(self):
        _, body = self.fetch("/settings")
        for theme in self.themes:
            with self.subTest(theme=theme):
                self.assertIn("Catppuccin " + theme, body)

    def test_theme_stylesheets_are_served(self):
        for theme in self.themes:
            with self.subTest(theme=theme):
                content_type, body = self.fetch(f"/css/themes/catppuccin_{theme.lower()}.css")
                self.assertEqual(content_type, "text/css")
                self.assertIn("--bg_color:", body)
                self.assertIn(".poll-meter.leader", body)

    def test_theme_can_be_selected(self):
        for theme in self.themes:
            with self.subTest(theme=theme):
                query = urlencode({"theme": "Catppuccin " + theme})
                _, body = self.fetch("/settings?" + query)
                self.assertIn(f'/css/themes/catppuccin_{theme.lower()}.css', body)

    def test_generated_css_and_embed_scripts_are_served(self):
        for path in ("/css/style.css", "/js/widgets.js", "/js/embedResize.js"):
            with self.subTest(path=path):
                _, body = self.fetch(path)
                self.assertTrue(body.strip())


if __name__ == "__main__":
    unittest.main()
