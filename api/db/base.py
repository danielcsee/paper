"""Engine and session plumbing."""

from __future__ import annotations

from contextlib import contextmanager
from typing import Iterator

from sqlalchemy import create_engine
from sqlalchemy.engine import Engine, make_url
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

from api.app.config import get_settings


class Base(DeclarativeBase):
    pass


def normalise_url(url: str) -> str:
    """Force the psycopg3 driver.

    Bare `postgresql://` makes SQLAlchemy reach for psycopg2, which we do not
    install; the DSN in .env stays driver-agnostic for non-Python consumers.
    """
    parsed = make_url(url)
    if parsed.drivername == "postgresql":
        parsed = parsed.set(drivername="postgresql+psycopg")
    return parsed.render_as_string(hide_password=False)


_engine: Engine | None = None
_sessionmaker: sessionmaker[Session] | None = None


def get_engine() -> Engine:
    global _engine
    if _engine is None:
        _engine = create_engine(
            normalise_url(get_settings().database_url), pool_pre_ping=True, future=True
        )
    return _engine


def get_sessionmaker() -> sessionmaker[Session]:
    global _sessionmaker
    if _sessionmaker is None:
        _sessionmaker = sessionmaker(bind=get_engine(), expire_on_commit=False)
    return _sessionmaker


@contextmanager
def session_scope() -> Iterator[Session]:
    """Commit on success, roll back on failure, always close."""
    session = get_sessionmaker()()
    try:
        yield session
        session.commit()
    except Exception:
        session.rollback()
        raise
    finally:
        session.close()
