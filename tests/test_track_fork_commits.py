import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))

import track_fork_commits as tracker


class ForkTrackerTest(unittest.TestCase):
    def test_follows_remote_head_and_lists_missed_commits_in_order(self):
        with tempfile.TemporaryDirectory() as directory:
            origin = Path(directory) / "source.git"

            def git(*args):
                return tracker.run("git", "-C", str(origin), *args)

            origin.mkdir()
            git("init", "--quiet", "-b", "master")
            git("config", "commit.gpgsign", "false")
            git("config", "user.name", "Test")
            git("config", "user.email", "test@example.test")

            def commit(subject):
                git("commit", "--quiet", "--allow-empty", "-m", subject)
                return git("rev-parse", "HEAD")

            url = origin.as_uri().removesuffix(".git")
            initial = commit("Existing")
            head, summary, entries = tracker.fork_update(url, None)
            self.assertEqual(head, initial)
            self.assertIn("Started tracking", summary)
            self.assertEqual(entries, [])
            self.assertEqual(tracker.fork_update(url, initial), (initial, "", []))

            git("branch", "-m", "main")
            first = commit("Handle <markup> & @mentions")
            second = commit("Second update")
            head, summary, entries = tracker.fork_update(url, initial)
            self.assertEqual(head, second)
            self.assertIn(f"/compare/{initial}...{second}", summary)
            self.assertEqual(entries, [
                f"- [{first[:8]}]({url}/commit/{first}) "
                "<code>Handle &lt;markup&gt; &amp; @mentions</code>",
                f"- [{second[:8]}]({url}/commit/{second}) <code>Second update</code>",
            ])
            self.assertEqual(tracker.fork_update(url, second), (second, "", []))

    def test_only_reads_checkpoint_from_latest_bot_comment(self):
        old = {"cmj/nitter": "old"}
        new = {"cmj/nitter": "new", "shitter/shitter": "head"}

        def comment(heads, author=tracker.BOT):
            return {"body": tracker.make_comment(heads, []), "user": {"login": author}}

        self.assertEqual(tracker.last_reported_heads([]), {})
        self.assertEqual(
            tracker.last_reported_heads([
                comment(old), comment(new), comment(old, "other-user")
            ]),
            new,
        )

    @patch.dict("os.environ", {"GITHUB_REPOSITORY": "owner/repo", "TRACKER_ISSUE": "38"})
    @patch.object(tracker, "fork_update")
    @patch.object(tracker, "run")
    def test_posts_healthy_fork_and_preserves_checkpoint_for_failed_fork(self, run, update):
        old = {"cmj/nitter": "old-cmj", "shitter/shitter": "old-shitter"}
        run.side_effect = [
            json.dumps([[{"body": tracker.make_comment(old, []),
                          "user": {"login": tracker.BOT}}]]),
            "{}",
        ]
        update.side_effect = [
            subprocess.CalledProcessError(1, ["git"]),
            ("new-cmj", "Compare commits", ["- Newly published commit"]),
        ]

        with self.assertRaisesRegex(SystemExit, "shitter/shitter"):
            tracker.main()

        self.assertEqual(run.call_count, 2)
        body = json.loads(run.call_args.kwargs["input"])["body"]
        self.assertIn("- Newly published commit", body)
        self.assertEqual(
            tracker.last_reported_heads([{"body": body, "user": {"login": tracker.BOT}}]),
            {"cmj/nitter": "new-cmj", "shitter/shitter": "old-shitter"},
        )
