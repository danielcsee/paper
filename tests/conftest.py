"""Put the repository root on `sys.path` so tests import `api` as the app does.

The package is not installed into the virtualenv — it is run from the checkout
by `scripts/dev.sh` — so `api` is importable only when the root is on the path.
"""

from __future__ import annotations

import sys
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))
