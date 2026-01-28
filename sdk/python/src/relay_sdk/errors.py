"""Error types for Relay SDK."""

from typing import Any, Optional


class RelayError(Exception):
    """Base error class for Relay SDK."""

    pass


class APIError(RelayError):
    """Error returned by the Relay API."""

    def __init__(
        self,
        message: str,
        status_code: int,
        code: Optional[str] = None,
        details: Optional[dict[str, Any]] = None,
    ) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.code = code
        self.details = details

    def __str__(self) -> str:
        if self.code:
            return f"relay: {self.message} (HTTP {self.status_code}, code: {self.code})"
        return f"relay: {self.message} (HTTP {self.status_code})"

    @classmethod
    def from_response(cls, status_code: int, body: Any) -> "APIError":
        """Create from API response."""
        if isinstance(body, dict):
            return cls(
                message=body.get("error", f"HTTP {status_code}"),
                status_code=status_code,
                code=body.get("code"),
                details=body.get("details"),
            )
        return cls(message=str(body) if body else f"HTTP {status_code}", status_code=status_code)

    def is_unauthorized(self) -> bool:
        """Check if this is an authorization error."""
        return self.status_code == 401

    def is_not_found(self) -> bool:
        """Check if this is a not found error."""
        return self.status_code == 404

    def is_conflict(self) -> bool:
        """Check if this is a conflict error."""
        return self.status_code == 409

    def is_rate_limited(self) -> bool:
        """Check if this is a rate limit error."""
        return self.status_code == 429

    def is_bad_request(self) -> bool:
        """Check if this is a bad request error."""
        return self.status_code in (400, 422)

    def is_server_error(self) -> bool:
        """Check if this is a server error."""
        return self.status_code >= 500


class SignatureError(RelayError):
    """Error during signature verification."""

    pass


class InvalidSignatureError(SignatureError):
    """Signature does not match."""

    def __init__(self) -> None:
        super().__init__("Signature mismatch")


class MissingSignatureError(SignatureError):
    """Signature header is missing."""

    def __init__(self) -> None:
        super().__init__("Missing signature header")


class InvalidTimestampError(SignatureError):
    """Timestamp is invalid or expired."""

    def __init__(self, message: str = "Invalid or expired timestamp") -> None:
        super().__init__(message)


class MissingTimestampError(SignatureError):
    """Timestamp header is missing."""

    def __init__(self) -> None:
        super().__init__("Missing timestamp header")
