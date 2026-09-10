"""Bundled pytest plugin that writes a stable, compact JSON result artifact."""

import json
import os
import time


_started_at = time.time()
_reports = {}
_collection_errors = []


def _coverage_context(nodeid):
    try:
        import coverage
        current = coverage.Coverage.current()
        if current is not None:
            current.switch_context(nodeid)
    except Exception:
        pass


def pytest_runtest_protocol(item, nextitem):
    _coverage_context(item.nodeid)


def pytest_runtest_logreport(report):
    entry = _reports.setdefault(report.nodeid, {
        "test_key": report.nodeid,
        "outcome": "passed",
        "phase": "call",
        "duration_seconds": 0.0,
        "message": "",
        "traceback": "",
        "stdout": "",
        "stderr": "",
    })
    entry["duration_seconds"] += float(getattr(report, "duration", 0.0))
    if report.skipped and entry["outcome"] == "passed":
        entry["outcome"] = "skipped"
        entry["phase"] = report.when
    elif report.failed:
        entry["outcome"] = "failed" if report.when == "call" else "error"
        entry["phase"] = report.when
        text = getattr(report, "longreprtext", str(report.longrepr))
        entry["traceback"] = text
        entry["message"] = text.splitlines()[-1] if text else "test failed"
    for name, content in getattr(report, "sections", []):
        if "stdout" in name:
            entry["stdout"] += content
        elif "stderr" in name:
            entry["stderr"] += content


def pytest_collectreport(report):
    if report.failed:
        text = getattr(report, "longreprtext", str(report.longrepr))
        _collection_errors.append({
            "test_key": getattr(report, "nodeid", "<collection>"),
            "outcome": "error",
            "phase": "collection",
            "duration_seconds": 0.0,
            "message": text.splitlines()[-1] if text else "collection failed",
            "traceback": text,
            "stdout": "",
            "stderr": "",
        })


def pytest_sessionfinish(session, exitstatus):
    output = os.environ.get("TESTLEDGER_PYTEST_JSON")
    if not output:
        return
    results = list(_reports.values()) + _collection_errors
    payload = {
        "schema_version": 1,
        "started_at_unix": _started_at,
        "finished_at_unix": time.time(),
        "exit_status": int(exitstatus),
        "tests": sorted(results, key=lambda item: item["test_key"]),
    }
    temporary = output + ".tmp"
    with open(temporary, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, sort_keys=True)
    os.replace(temporary, output)
