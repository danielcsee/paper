"""`api.ncbi.http.RateLimiter` — the in-process NCBI request budget.

This is the fallback that runs whenever Redis is unreachable, so it is the
thing standing between us and NCBI's 3/s limit at exactly the moment the
shared limiter is unavailable. Getting it wrong is not a local failure: it is
an external party throttling or blocking the project.

The clock and the sleep are both replaced, so the spacing arithmetic is
asserted directly and the suite never actually waits.
"""

from __future__ import annotations

import asyncio
import types
from typing import List

import pytest

from api.ncbi import http
from api.ncbi.http import NCBI_REQUESTS_PER_SECOND, RateLimiter


class FakeClock:
    """A monotonic clock that only moves when something sleeps."""

    def __init__(self, start: float = 1_000.0) -> None:
        self.now = start
        self.slept: List[float] = []

    def monotonic(self) -> float:
        return self.now

    async def sleep(self, delay: float) -> None:
        self.slept.append(delay)
        self.now += delay


@pytest.fixture
def clock(monkeypatch: pytest.MonkeyPatch) -> FakeClock:
    """Replace the module's own `time` and `asyncio` names, not the real ones."""
    fake = FakeClock()
    monkeypatch.setattr(http, "time", fake)
    monkeypatch.setattr(http, "asyncio", types.SimpleNamespace(sleep=fake.sleep))
    return fake


def test_rate_limiter_rejects_a_nonpositive_rate() -> None:
    """A zero rate would mean an infinite interval; a negative one, time travel."""
    for rate in (0.0, -1.0, -0.5):
        with pytest.raises(ValueError):
            RateLimiter(rate)


def test_rate_limiter_spaces_slots_evenly(clock: FakeClock) -> None:
    """Strict spacing, not a token bucket: three requests cannot share a moment.

    A bucket is compliant only if the server counts in aligned one-second
    windows. Spacing every request 1/rate apart is under the limit under any
    accounting, which is why this asserts the gaps and not the count.
    """
    limiter = RateLimiter(NCBI_REQUESTS_PER_SECOND)
    interval = 1.0 / NCBI_REQUESTS_PER_SECOND
    starts: List[float] = []

    async def drive() -> None:
        for _ in range(6):
            await limiter.acquire()
            starts.append(clock.now)

    asyncio.run(drive())

    # The first caller does not wait; every one after it waits a full interval.
    assert clock.slept == pytest.approx([interval] * 5)
    gaps = [b - a for a, b in zip(starts, starts[1:])]
    assert gaps == pytest.approx([interval] * 5)


def test_rate_limiter_does_not_burst_after_an_idle_gap(clock: FakeClock) -> None:
    """Idle time must not bank credit, or the first burst back breaks the limit.

    `max(now, self._next_slot)` is the line under test: a slot that has already
    passed is discarded rather than replayed.
    """
    limiter = RateLimiter(NCBI_REQUESTS_PER_SECOND)
    interval = 1.0 / NCBI_REQUESTS_PER_SECOND

    async def drive() -> None:
        await limiter.acquire()
        # Ten seconds of silence: thirty slots would have accrued in a bucket.
        clock.now += 10.0
        await limiter.acquire()
        await limiter.acquire()

    asyncio.run(drive())

    # The call after the idle gap goes immediately; the one after it still waits.
    assert clock.slept == pytest.approx([interval])


def test_rate_limiter_claims_its_slot_before_sleeping(clock: FakeClock) -> None:
    """Concurrent callers must queue, not collide on the same slot.

    `acquire` reserves the slot under the lock and sleeps afterwards, so two
    coroutines that start together still leave one interval apart. Holding the
    slot only while sleeping would hand both the same one.
    """
    limiter = RateLimiter(NCBI_REQUESTS_PER_SECOND)
    interval = 1.0 / NCBI_REQUESTS_PER_SECOND
    finished: List[float] = []

    async def caller() -> None:
        await limiter.acquire()
        finished.append(clock.now)

    async def drive() -> None:
        await asyncio.gather(caller(), caller(), caller())

    asyncio.run(drive())

    assert sorted(finished) == pytest.approx([1_000.0, 1_000.0 + interval, 1_000.0 + 2 * interval])


def test_rate_limiter_honours_a_rate_other_than_the_ncbi_default(clock: FakeClock) -> None:
    """The interval is derived from the rate, not hard-coded to NCBI's."""
    cases: list[tuple[float, float]] = [(1.0, 1.0), (2.0, 0.5), (10.0, 0.1)]

    for rate, expected_interval in cases:
        fresh = FakeClock()
        clock.now = fresh.now
        clock.slept.clear()
        limiter = RateLimiter(rate)

        async def drive() -> None:
            await limiter.acquire()
            await limiter.acquire()

        asyncio.run(drive())

        assert clock.slept == pytest.approx([expected_interval]), rate
        clock.slept.clear()
