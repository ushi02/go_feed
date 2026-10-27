"""Optional network smoke test against the public Go blog Atom feed."""

import subprocess
import time
import uuid

from smoke import call, client, sql


def main():
    user_id = None
    opener = client()
    suffix = uuid.uuid4().hex[:10]
    try:
        _, user = call(opener, "POST", "/api/auth/register", {"username": "live" + suffix, "password": "smoke-password-123"})
        user_id = user["id"]
        _, found = call(opener, "GET", "/api/discover?url=https%3A%2F%2Fgo.dev%2Fblog")
        candidates = found["candidates"]
        target = next((x["url"] for x in candidates if "feed.atom" in x["url"]), None)
        assert target, candidates
        call(opener, "POST", "/api/subscriptions", {"url": target})
        subprocess.run(["docker", "compose", "restart", "worker"], check=True, capture_output=True)
        for _ in range(30):
            _, page = call(opener, "GET", "/api/entries")
            if page["entries"]:
                print(f"live feed smoke passed: discovered {target}, fetched {len(page['entries'])} visible articles")
                return
            time.sleep(2)
        raise AssertionError("worker did not import articles within 60 seconds")
    finally:
        if user_id is not None:
            sql(f"DELETE FROM users WHERE id={user_id};")


if __name__ == "__main__":
    main()
