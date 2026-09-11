"""`api.auth.throttle.SlidingWindow` — the cap on pre-authentication work.

`/auth/login` spends ~200 ms of CPU on PBKDF2 *before* the caller has proved
anything, so this window is what stops a few hundred requests a second against
the one username that exists from saturating a task. A silent regression here
does not fail a request; it removes a denial-of-service bound.

The clock is replaced so window expiry is asserted exactly, with no sleeping
and no flakiness on a loaded machine.
"""

from __future__ import annotations

from typing import List, Optional

import pytest

from api.auth import throttle
from api.auth.throttle import MAX_TRACKED_KEYS, SlidingWindow

_LIMIT = 3
_WINDOW = 60.0


class FakeClock:
    """A monotonic clock the test advances explicitly."""

    def __init__(self, start: float = 1_000.0) -> None:
        self.now = start

    def monotonic(self) -> float:
        return self.now

    def advance(self, seconds: float) -> None:
        self.now += seconds


@pytest.fixture
def clock(monkeypatch: pytest.MonkeyPatch) -> FakeClock:
    """Replace the module's own `time` name, not the real module."""
    fake = FakeClock()
    monkeypatch.setattr(throttle, "time", fake)
    return fake


@pytest.fixture
def window(clock: FakeClock) -> SlidingWindow:
    return SlidingWindow(_LIMIT, _WINDOW)


def test_sliding_window_allows_up_to_the_limit(window: SlidingWindow) -> None:
    """The first `limit` events are allowed, and allowed means None."""
    results: List[Optional[float]] = [window.retry_after("client") for _ in range(_LIMIT)]

    assert results == [None] * _LIMIT


def test_sliding_window_reports_the_wait_once_over(window: SlidingWindow) -> None:
    """Over the limit returns the seconds until the oldest hit falls out."""
    for _ in range(_LIMIT):
        window.retry_after("client")

    assert window.retry_after("client") == pytest.approx(_WINDOW)


def test_sliding_window_does_not_extend_its_own_lockout(
    window: SlidingWindow, clock: FakeClock
) -> None:
    """A rejected event is not recorded, so hammering cannot become a ban.

    Recording rejections would push the oldest hit forward on every retry and
    turn a burst of client retries into an indefinite lockout.
    """
    for _ in range(_LIMIT):
        window.retry_after("client")

    clock.advance(30.0)
    for _ in range(50):
        window.retry_after("client")

    # Still keyed off the original hits: 30 of the 60 seconds have elapsed.
    assert window.retry_after("client") == pytest.approx(_WINDOW - 30.0)


def test_sliding_window_frees_budget_as_events_age_out(
    window: SlidingWindow, clock: FakeClock
) -> None:
    """A deque of timestamps, not a counter with a reset.

    The limit cannot be doubled by straddling a boundary, and the budget comes
    back one event at a time rather than all at once.
    """
    start = clock.now
    # Spaced a second apart, so they do not all expire in the same instant.
    for _ in range(_LIMIT):
        assert window.retry_after("client") is None
        clock.advance(1.0)
    assert window.retry_after("client") is not None

    # Just past the *first* hit's expiry: one slot returns, not the whole budget.
    clock.now = start + _WINDOW + 0.5
    assert window.retry_after("client") is None
    assert window.retry_after("client") is not None


def test_sliding_window_keeps_keys_independent(window: SlidingWindow) -> None:
    """The per-address and per-username keys must not share one budget."""
    for _ in range(_LIMIT):
        assert window.retry_after("address:198.51.100.7") is None

    assert window.retry_after("address:198.51.100.7") is not None
    assert window.retry_after("user:alice") is None


def test_sliding_window_retry_after_never_returns_a_negative_wait(
    window: SlidingWindow, clock: FakeClock
) -> None:
    """`max(0.0, ...)` guards the boundary where the oldest hit is expiring."""
    for _ in range(_LIMIT):
        window.retry_after("client")

    clock.advance(_WINDOW)
    result = window.retry_after("client")

    assert result is None or result >= 0.0


def test_sliding_window_reset_forgets_every_key(window: SlidingWindow) -> None:
    """`reset` is for tests; it must clear the map, not just one key."""
    for _ in range(_LIMIT):
        window.retry_after("client")
    assert window.retry_after("client") is not None

    window.reset()

    assert window.retry_after("client") is None


def test_sliding_window_bounds_the_key_map(clock: FakeClock) -> None:
    """Spoofed addresses must not grow the map without limit.

    The least recently seen key is dropped, which at worst hands an attacker a
    fresh budget they already had — the memory bound is the point.

    Both halves of `_prune` run here: an aged-out key is deleted outright, and
    the remainder are evicted least-recently-used once the map is full.
    """
    window = SlidingWindow(limit=1, window_seconds=_WINDOW)

    assert window.retry_after("aged-out") is None
    clock.advance(_WINDOW * 2)

    for index in range(MAX_TRACKED_KEYS + 1):
        assert window.retry_after(f"key-{index}") is None

    # Expired rather than evicted: dropped on the first call after its window.
    assert "aged-out" not in window._hits

    assert len(window._hits) == MAX_TRACKED_KEYS
    # The oldest key was evicted, so its budget is fresh again.
    assert window.retry_after("key-0") is None
    # One seen recently is still tracked, and still spent.
    assert window.retry_after(f"key-{MAX_TRACKED_KEYS}") is not None


def test_sliding_window_prunes_keys_whose_events_have_expired(
    window: SlidingWindow, clock: FakeClock
) -> None:
    """An idle key is dropped rather than held forever."""
    assert window.retry_after("stale") is None
    assert "stale" in window._hits

    clock.advance(_WINDOW * 2)
    assert window.retry_after("fresh") is None

    assert "stale" not in window._hits
    assert "fresh" in window._hits
