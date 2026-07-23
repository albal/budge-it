#!/usr/bin/env python3
"""Simulated budge-it user journeys, driven by the loadsim CronJob.

Each simulated user (random @void.com email) walks the real product flow
through the public API Route: log in, upload a randomly generated CSV bank
statement, wait for async processing, annotate a few transactions with new
categories, then review the analytics. Accounts are purged by the companion
cleanup CronJob once they are older than two hours.

Standard library only: the pod is a stock UBI Python image with no deps.
"""
import concurrent.futures
import http.cookiejar
import io
import json
import os
import random
import ssl
import sys
import time
import urllib.error
import urllib.request
import uuid
from datetime import date, timedelta

API = os.environ.get("API_URL", "https://budgeit-api-budge-it.apps.budgie.lab.tsew.net").rstrip("/")
USERS = int(os.environ.get("USERS", "50"))
CONCURRENCY = int(os.environ.get("CONCURRENCY", "10"))

# The lab router serves the default wildcard cert; don't verify.
SSL_CTX = ssl.create_default_context()
SSL_CTX.check_hostname = False
SSL_CTX.verify_mode = ssl.CERT_NONE

MERCHANTS = [
    ("TESCO STORES 3297", -95, -20), ("SAINSBURYS S/MKT", -80, -15),
    ("NETFLIX.COM", -16, -16), ("SPOTIFY AB", -11, -11),
    ("AMZN MKTPLACE*0442", -120, -8), ("UBER *TRIP HELP.UBER.COM", -35, -6),
    ("STARBUCKS 1042", -8, -3), ("BRITISH GAS ENERGY", -140, -60),
    ("COUNCIL TAX", -180, -120), ("SHELL PETROL 442", -90, -30),
    ("DELIVEROO LONDON", -45, -12), ("BOOTS PHARMACY 118", -30, -5),
    ("ATM WITHDRAWAL HIGH ST", -100, -20), ("GREGGS BAKERY", -9, -2),
    ("TFL TRAVEL CH", -12, -3), ("VODAFONE LTD", -45, -20),
]


def make_statement():
    """A plausible random statement: one salary credit + 12-30 debits."""
    out = io.StringIO()
    out.write("Date,Description,Amount\n")
    today = date.today()
    payday = today - timedelta(days=random.randint(1, 28))
    out.write(f"{payday},SALARY ACME CORP,{random.randint(2200, 4500)}.00\n")
    for _ in range(random.randint(12, 30)):
        merchant, lo, hi = random.choice(MERCHANTS)
        d = today - timedelta(days=random.randint(0, 60))
        amount = round(random.uniform(lo, hi), 2)
        out.write(f"{d},{merchant},{amount}\n")
    return out.getvalue().encode()


class Client:
    """Tiny cookie-aware JSON/HTTP client for one simulated user."""

    def __init__(self):
        jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(jar),
            urllib.request.HTTPSHandler(context=SSL_CTX),
        )

    def request(self, method, path, body=None, content_type="application/json"):
        req = urllib.request.Request(API + path, data=body, method=method)
        if body is not None:
            req.add_header("Content-Type", content_type)
        with self.opener.open(req, timeout=30) as resp:
            data = resp.read()
        return json.loads(data) if data else None

    def json(self, method, path, obj):
        return self.request(method, path, json.dumps(obj).encode())

    def upload(self, filename, content):
        boundary = uuid.uuid4().hex
        body = (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            "Content-Type: text/csv\r\n\r\n"
        ).encode() + content + f"\r\n--{boundary}--\r\n".encode()
        return self.request("POST", "/api/v1/uploads", body,
                            f"multipart/form-data; boundary={boundary}")


def journey(i):
    email = f"sim-{uuid.uuid4().hex[:12]}@void.com"
    c = Client()

    c.json("POST", "/api/v1/auth/login", {"email": email})

    up = c.upload("statement.csv", make_statement())
    deadline = time.time() + 120
    while time.time() < deadline:
        up = c.request("GET", f"/api/v1/uploads/{up['id']}")
        if up["status"] in ("done", "error"):
            break
        time.sleep(1.5)
    if up["status"] != "done":
        raise RuntimeError(f"upload never finished: {up['status']} {up.get('error', '')}")

    # Annotate: re-categorize a few transactions, which also persists rules.
    txns = c.request("GET", "/api/v1/transactions")
    categories = c.request("GET", "/api/v1/categories")
    for txn in random.sample(txns, k=min(len(txns), random.randint(1, 3))):
        c.json("PATCH", f"/api/v1/transactions/{txn['id']}",
               {"category": random.choice(categories)})

    # Review: the dashboard's read path.
    summary = c.request("GET", "/api/v1/analytics/summary")
    c.request("GET", "/api/v1/analytics/categories")
    month = summary["months"][-1]["month"] if summary.get("months") else ""
    c.request("GET", f"/api/v1/transactions?month={month}")

    return f"user {i} ({email}): {up['txnCount']} txns processed"


def main():
    started = time.time()
    ok = failed = 0
    with concurrent.futures.ThreadPoolExecutor(max_workers=CONCURRENCY) as pool:
        futures = {pool.submit(journey, i): i for i in range(USERS)}
        for fut in concurrent.futures.as_completed(futures):
            try:
                print(fut.result(), flush=True)
                ok += 1
            except Exception as err:  # noqa: BLE001 - one bad journey shouldn't kill the run
                failed += 1
                print(f"user {futures[fut]} FAILED: {err}", flush=True)
    print(f"run complete: ok={ok} failed={failed} elapsed={time.time() - started:.0f}s")
    sys.exit(1 if ok == 0 else 0)


if __name__ == "__main__":
    main()
