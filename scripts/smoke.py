"""Local API/MySQL smoke test. Run after `docker compose up -d --build`."""

import http.cookiejar
import json
import os
import subprocess
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("API_URL", "http://127.0.0.1:18080")
ORIGIN = BASE  # the frontend is served by the API, so writes are same-origin


def client():
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))


def call(opener, method, path, body=None, content_type="application/json"):
    data = None if body is None else (json.dumps(body).encode() if content_type == "application/json" else body)
    req = urllib.request.Request(BASE + path, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", content_type)
    if method not in ("GET", "HEAD"):
        req.add_header("Origin", ORIGIN)
    try:
        with opener.open(req, timeout=15) as response:
            payload = response.read()
            return response.status, json.loads(payload) if payload and response.headers.get_content_type() == "application/json" else payload
    except urllib.error.HTTPError as error:
        raise AssertionError(f"{method} {path}: HTTP {error.code}: {error.read().decode()}") from error


def sql(query):
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "mysql", "mysql", "-uroot", "-p123456", "go_feed"],
        input=query, text=True, capture_output=True, check=False,
    )
    if result.returncode:
        raise RuntimeError(result.stderr)


def main():
    suffix = uuid.uuid4().hex[:10]
    users = []
    feed_id = None
    try:
        first, second = client(), client()
        for opener, prefix in ((first, "a"), (second, "b")):
            status, user = call(opener, "POST", "/api/auth/register", {"username": prefix + suffix, "password": "smoke-password-123"})
            assert status == 201
            users.append(user["id"])
        _, folder = call(first, "POST", "/api/folders", {"name": "技术"})
        _, sub = call(first, "POST", "/api/subscriptions", {"url": f"https://example.invalid/{suffix}/rss", "folder_id": folder["id"]})
        feed_id = sub["feed_id"]
        values = ",".join(
            f"({feed_id},UNHEX(SHA2('smoke-{suffix}-{i}',256)),'Article {i}','https://example.invalid/article/{i}','<p>body</p>',UTC_TIMESTAMP(6))"
            for i in range(25)
        )
        sql(f"UPDATE feeds SET initialized=1,next_fetch_at=DATE_ADD(UTC_TIMESTAMP(6), INTERVAL 1 DAY) WHERE id={feed_id};"
            f"INSERT INTO entries(feed_id,guid_hash,title,url,content,sort_at) VALUES {values};")
        _, second_sub = call(second, "POST", "/api/subscriptions", {"url": f"https://example.invalid/{suffix}/rss"})
        assert second_sub["feed_id"] == feed_id
        _, page = call(second, "GET", "/api/entries?limit=20&filter=unread")
        assert len(page["entries"]) == 20 and page["next_cursor"] == "", page
        # Fan-out on write: a new subscriber gets only the 20 newest entries, not all 25.
        _, first_page = call(second, "GET", "/api/entries?limit=15")
        assert len(first_page["entries"]) == 15 and first_page["next_cursor"]
        _, next_page = call(second, "GET", "/api/entries?limit=15&cursor=" + first_page["next_cursor"])
        assert len(next_page["entries"]) == 5 and next_page["next_cursor"] == "", next_page
        call(second, "POST", "/api/entries/mark-read", {})
        _, unread = call(second, "GET", "/api/entries?filter=unread")
        assert unread["entries"] == [], unread
        entry_id = first_page["entries"][-1]["id"]
        call(second, "PATCH", f"/api/entries/{entry_id}/state", {"read": False, "starred": True})
        _, unread = call(second, "GET", "/api/entries?filter=unread")
        _, starred = call(second, "GET", "/api/entries?filter=starred")
        assert len(unread["entries"]) == 1 and len(starred["entries"]) == 1
        _, export = call(first, "GET", "/api/opml/export")
        assert b"<opml" in export and b"xmlUrl" in export
        boundary = "smoke-boundary"
        body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"subscriptions.opml\"\r\n"
                "Content-Type: text/xml\r\n\r\n").encode() + export + f"\r\n--{boundary}--\r\n".encode()
        _, imported = call(first, "POST", "/api/opml/import", body, f"multipart/form-data; boundary={boundary}")
        assert imported["added"] == 0 and imported["skipped"] >= 1, imported
        print("smoke test passed: auth, folder, shared source, 20 unread, cursor, bulk read, overrides, OPML round trip")
    finally:
        if users:
            sql("DELETE FROM users WHERE id IN (" + ",".join(map(str, users)) + ");")
        if feed_id is not None:
            sql(f"DELETE FROM feeds WHERE id={feed_id};")


if __name__ == "__main__":
    main()
