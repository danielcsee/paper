"""Errors raised by the NCBI clients, mapped to HTTP status codes in their routes."""


class NcbiError(Exception):
    """Base class. `status` is what the API should return to its caller."""

    status = 502

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message


class NotFoundError(NcbiError):
    """The requested article does not exist, or is not in the open-access set."""

    status = 404


class UpstreamError(NcbiError):
    """NCBI returned an error, or could not be reached."""

    status = 502


class InvalidRequestError(NcbiError):
    """The caller's arguments are malformed."""

    status = 400
