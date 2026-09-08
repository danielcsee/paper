"""Access control tables: users, their sessions, and the free access codes.

Three tables, and the interesting part is the constraints between them.

`users` holds exactly two rows in practice — `admin` and `anonfree` — because
this app has no user-owned data. Papers, chunks, entities and the graph are one
global pool; a user is a key to the expensive doors, not an owner of anything.

`auth_sessions` is one row per live refresh-token family. Several rows per user
is the point: two browsers means two sessions.

The rule "anonfree can never hold more live tokens than there are activated
access codes" is enforced *structurally*, not by counting:

* every anonfree session must name the code that minted it
  (`ck_auth_sessions_anon_has_code`), and
* a code can back at most one un-revoked session
  (`uq_auth_sessions_live_code`, a partial unique index), and
* the code it names must already be activated (the composite foreign key to
  `(id, activated)` plus `ck_auth_sessions_code_activated`).

Postgres cannot put a subquery in a CHECK, so a literal `count(tokens) <=
count(codes)` is not expressible. Carrying the two flags — `user_is_anonymous`
and `code_activated` — as columns validated by composite foreign keys makes the
cross-table facts row-local, which is what lets the CHECKs above see them. The
columns are redundant by design: the foreign keys guarantee they cannot drift
from the rows they mirror.
"""

from __future__ import annotations

import datetime as dt
from typing import Optional

from sqlalchemy import (
    BigInteger,
    Boolean,
    CheckConstraint,
    DateTime,
    ForeignKeyConstraint,
    Index,
    String,
    Text,
    UniqueConstraint,
    func,
    text,
)
from sqlalchemy.orm import Mapped, mapped_column

from api.db.base import Base

#: The two seeded accounts. Referenced by name rather than id, so a re-seeded
#: database cannot silently point the app at the wrong row.
ADMIN_USERNAME = "admin"
ANON_USERNAME = "anonfree"


class User(Base):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    username: Mapped[str] = mapped_column(String(64), unique=True, nullable=False)
    #: Null means "no password login". `anonfree` is reachable only by
    #: redeeming an access code, so it must never be guessable by password.
    password_hash: Mapped[Optional[str]] = mapped_column(Text)
    is_admin: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)
    #: Mirrored onto `auth_sessions.user_is_anonymous` through a composite FK,
    #: so the session table can CHECK it without a subquery.
    is_anonymous: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)

    created_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    #: Set to lock an account out. Checked when a token is minted or refreshed.
    disabled_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))

    __table_args__ = (
        # The target of auth_sessions' composite FK. Redundant against the
        # primary key, and required: Postgres will only point a foreign key at
        # a uniquely-constrained set of columns.
        UniqueConstraint("id", "is_anonymous", name="uq_users_id_is_anonymous"),
        # At most one admin, ever. Unique on a column filtered to the rows
        # where it is true: two admin rows would both index the value `true`
        # and collide. "Only one admin may exist" is then a fact about the
        # table rather than a rule the create endpoint has to remember.
        Index(
            "uq_users_single_admin",
            "is_admin",
            unique=True,
            postgresql_where=text("is_admin"),
        ),
        CheckConstraint(
            "NOT (is_admin AND is_anonymous)", name="ck_users_role_exclusive"
        ),
        # An anonymous account with a password would be a second way in that
        # bypasses the code accounting entirely.
        CheckConstraint(
            "NOT (is_anonymous AND password_hash IS NOT NULL)",
            name="ck_users_anonymous_has_no_password",
        ),
    )


class FreeAccessCode(Base):
    __tablename__ = "free_access_codes"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    code: Mapped[str] = mapped_column(String(128), unique=True, nullable=False)
    activated: Mapped[bool] = mapped_column(
        Boolean, nullable=False, server_default=text("false")
    )
    creation_date: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    #: Set once, on first redemption, and never restamped — re-entering a live
    #: code must not extend its window, or the 48-hour cap is unreachable for
    #: anyone who logs in daily.
    activation_date: Mapped[Optional[dt.datetime]] = mapped_column(
        DateTime(timezone=True)
    )

    __table_args__ = (
        UniqueConstraint("id", "activated", name="uq_free_access_codes_id_activated"),
        CheckConstraint(
            "activated = (activation_date IS NOT NULL)",
            name="ck_free_access_codes_activation_pair",
        ),
    )


class AuthSession(Base):
    __tablename__ = "auth_sessions"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    user_id: Mapped[int] = mapped_column(BigInteger, nullable=False)
    #: Mirror of users.is_anonymous, held true by the composite FK below.
    user_is_anonymous: Mapped[bool] = mapped_column(Boolean, nullable=False)

    #: sha256 of the opaque refresh token. The token itself is shown once, to
    #: the browser that earned it, and never stored.
    refresh_token_hash: Mapped[str] = mapped_column(
        String(64), unique=True, nullable=False
    )

    free_access_code_id: Mapped[Optional[int]] = mapped_column(BigInteger)
    #: Mirror of free_access_codes.activated, held true by the composite FK.
    code_activated: Mapped[Optional[bool]] = mapped_column(Boolean)

    issued_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    last_used_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))
    #: For a code-backed session this is activation_date + the code window, so
    #: the session cannot outlive the code even if the row is read directly.
    expires_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), nullable=False
    )
    revoked_at: Mapped[Optional[dt.datetime]] = mapped_column(DateTime(timezone=True))

    __table_args__ = (
        ForeignKeyConstraint(
            ["user_id", "user_is_anonymous"],
            ["users.id", "users.is_anonymous"],
            name="fk_auth_sessions_user",
            ondelete="CASCADE",
        ),
        ForeignKeyConstraint(
            ["free_access_code_id", "code_activated"],
            ["free_access_codes.id", "free_access_codes.activated"],
            name="fk_auth_sessions_code",
            ondelete="CASCADE",
        ),
        # anonfree sessions come from a code; admin sessions never do.
        CheckConstraint(
            "user_is_anonymous = (free_access_code_id IS NOT NULL)",
            name="ck_auth_sessions_anon_has_code",
        ),
        CheckConstraint(
            "(free_access_code_id IS NULL) = (code_activated IS NULL)",
            name="ck_auth_sessions_code_pair",
        ),
        # With the composite FK above, this is what forces the referenced code
        # to be an *activated* one: an unactivated code cannot back a session.
        CheckConstraint(
            "code_activated IS NOT FALSE", name="ck_auth_sessions_code_activated"
        ),
        # One live session per code. This is the seat: redeeming a code on a
        # second device moves the seat rather than adding one.
        Index(
            "uq_auth_sessions_live_code",
            "free_access_code_id",
            unique=True,
            postgresql_where=text(
                "free_access_code_id IS NOT NULL AND revoked_at IS NULL"
            ),
        ),
        Index("ix_auth_sessions_user_id", "user_id"),
    )


class AdminChallenge(Base):
    """A one-shot nonce for signature-authenticated admin calls.

    In Postgres rather than Redis: the auth routes are synchronous and
    `api.redis_conn` is async-only, and these are admin-rate, not request-rate.
    Single use is enforced by `DELETE ... RETURNING`, which is atomic — two
    requests racing the same nonce, exactly one gets the row.
    """

    __tablename__ = "admin_challenges"

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True)
    nonce: Mapped[str] = mapped_column(String(64), unique=True, nullable=False)
    #: Which endpoint the nonce was issued for. Checked on use, so a signature
    #: captured for one admin action cannot be redirected at another.
    action: Mapped[str] = mapped_column(String(32), nullable=False)
    created_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now(), nullable=False
    )
    expires_at: Mapped[dt.datetime] = mapped_column(
        DateTime(timezone=True), nullable=False
    )

    __table_args__ = (Index("ix_admin_challenges_expires_at", "expires_at"),)
