"""A small in-process rate limiter for the unauthenticated auth endpoints.

`/auth/login` verifies a password with PBKDF2 at 600,000 iterations, which is
right for storing a password and is roughly 200 ms of CPU per call. That cost
is paid *before* the caller has proved anything, so a few hundred requests a
second against the one username that exists will saturate a task. `/auth/redeem`
is cheaper but is the endpoint an attacker would use to guess access codes.

**In-process, on purpose.** The resource being protected is this process's CPU,
so capping it here bounds the harm directly, whatever a load balancer decides
to do with the request distribution. It also adds no dependency and no new
failure mode -- a shared Redis limiter would need a policy for "Redis is down",
and both answers to that are bad: fail open and the protection evaporates
exactly when the system is already struggling, fail closed and an unrelated
outage locks everybody out.

The trade is that N tasks allow N times the budget. That is fine for a cap
whose job is to keep any single process from melting, and it is worth knowing
rather than discovering.

Two keys per login attempt:

* **per client address** -- the usual shape, and the one that stops a single
  source. It is only meaningful when uvicorn runs with `--proxy-headers`
  behind a load balancer; without that, every request appears to come from the
  balancer and this key degrades into a global limit.
* **per username** -- which bounds the PBKDF2 work no matter how many addresses
  the attempts come from. This is the one that actually caps the CPU, and it
  keeps working when the address is unusable.
"""

from __future__ import annotations

import logging
import threading
import time
from collections import OrderedDict, deque
from typing import Deque, Optional

log = logging.getLogger(__name__)

#: Distinct keys held at once. Bounded so that spoofed addresses cannot grow
#: this map without limit; the least recently seen key is dropped, which at
#: worst gives an attacker a fresh budget they already had.
MAX_TRACKED_KEYS = 4096


class SlidingWindow:
    """Allow `limit` events per `window_seconds`, per key.

    A deque of timestamps rather than a counter with a reset, so the limit
    cannot be doubled by straddling a window boundary.
    """

    def __init__(self, limit: int, window_seconds: float) -> None:
        self._limit = limit
        self._window = window_seconds
        self._hits: "OrderedDict[str, Deque[float]]" = OrderedDict()
        self._lock = threading.Lock()

    def retry_after(self, key: str) -> Optional[float]:
        """Seconds to wait, or None when the event is allowed and recorded.

        Only allowed events are recorded. A caller already over the limit does
        not extend its own lockout by continuing to hammer, which would turn a
        burst of retries into an indefinite ban.
        """
        now = time.monotonic()
        cutoff = now - self._window
        with self._lock:
            hits = self._hits.get(key)
            if hits is None:
                hits = deque()
                self._hits[key] = hits
            self._hits.move_to_end(key)

            while hits and hits[0] <= cutoff:
                hits.popleft()

            if len(hits) >= self._limit:
                return max(0.0, hits[0] + self._window - now)

            hits.append(now)
            self._prune(cutoff)
            return None

    def _prune(self, cutoff: float) -> None:
        """Drop empty and least-recently-used keys. Called under the lock."""
        for key in [k for k, v in self._hits.items() if not v or v[-1] <= cutoff]:
            del self._hits[key]
        while len(self._hits) > MAX_TRACKED_KEYS:
            self._hits.popitem(last=False)

    def reset(self) -> None:
        """Forget every key. For tests, not for request handling."""
        with self._lock:
            self._hits.clear()
