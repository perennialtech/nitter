"""Post new default-branch commits to the fork tracker issue."""

import html
import json
import os
import subprocess
import tempfile


FORKS = {
    "shitter/shitter": "https://codeberg.org/shitter/shitter",
    "cmj/nitter": "https://github.com/cmj/nitter",
}
BOT = "github-actions[bot]"
MARKER = "<!-- fork-commit-tracker "
COMMENT_LIMIT = 65_536


def run(*command, **kwargs):
    return subprocess.check_output(
        command, encoding="utf-8", errors="replace", **kwargs
    ).rstrip("\n")


def last_reported_heads(comments):
    for comment in reversed(comments):
        marker = comment["body"].partition("\n")[0]
        if comment["user"]["login"] == BOT and marker.startswith(MARKER):
            return json.loads(marker.removeprefix(MARKER).removesuffix(" -->"))
    return {}


def commit_link(url, sha):
    return f"[{sha[:8]}]({url}/commit/{sha})"


def fork_update(url, previous):
    # Reading remote HEAD follows the default branch even if it is renamed.
    head = run("git", "ls-remote", "--exit-code", f"{url}.git", "HEAD").split()[0]
    if head == previous:
        return head, "", []
    if previous is None:
        return head, f"Started tracking at {commit_link(url, head)}.", []

    with tempfile.TemporaryDirectory() as directory:
        run("git", "init", "--bare", "--quiet", directory)

        def git(*args, **kwargs):
            return run("git", "-C", directory, *args, **kwargs)

        git("fetch", "--quiet", "--no-tags", f"{url}.git", "HEAD")
        # The branch may have advanced between ls-remote and fetch.
        head = git("rev-parse", "FETCH_HEAD")
        if head == previous:
            return head, "", []
        try:
            git("cat-file", "-e", f"{previous}^{{commit}}", stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            # A force push can remove the old tip from the default branch history.
            try:
                git("fetch", "--quiet", "--no-tags", f"{url}.git", previous)
            except subprocess.CalledProcessError:
                return head, (
                    "The previous tip is no longer available. "
                    f"Resumed tracking at {commit_link(url, head)}."
                ), []

        history = git(
            "log", "--reverse", "--topo-order", "--format=%H%x09%s", f"{previous}..{head}"
        )

    if not history:
        return head, f"The default branch moved back to {commit_link(url, head)}.", []

    entries = []
    for line in history.split("\n"):
        sha, _, subject = line.partition("\t")
        entries.append(f"- {commit_link(url, sha)} <code>{html.escape(subject)}</code>")
    summary = f"[Compare {previous[:8]}…{head[:8]}]({url}/compare/{previous}...{head})"
    return head, summary, entries


def make_comment(heads, updates):
    marker = f"{MARKER}{json.dumps(heads)} -->"
    summaries = []
    sections = []
    for name, summary, entries in updates:
        heading = f"### {name}\n\n{summary}"
        summaries.append(heading)
        sections.append(f"{heading}\n\n" + "\n".join(entries) if entries else heading)
    body = "\n\n".join([marker, *sections])
    if len(body.encode("utf-8")) > COMMENT_LIMIT:
        # GitHub caps comment bodies. Preserve the full range in the compare links.
        return "\n\n".join([marker, *summaries])
    return body


def main():
    repository = os.environ["GITHUB_REPOSITORY"]
    issue = os.environ["TRACKER_ISSUE"]
    endpoint = f"repos/{repository}/issues/{issue}/comments"
    pages = json.loads(run("gh", "api", "--paginate", "--slurp", f"{endpoint}?per_page=100"))
    heads = last_reported_heads([comment for page in pages for comment in page])
    updates = []
    failures = []
    for name, url in FORKS.items():
        try:
            head, summary, entries = fork_update(url, heads.get(name))
        except subprocess.CalledProcessError:
            failures.append(name)
            continue
        if summary:
            heads[name] = head
            updates.append((name, summary, entries))

    if updates:
        body = json.dumps({"body": make_comment(heads, updates)})
        run("gh", "api", "--method", "POST", endpoint, "--input", "-", input=body)
    if failures:
        raise SystemExit(f"Could not check {', '.join(failures)}")


if __name__ == "__main__":
    main()
