// Base error class for Relay SDK
export class RelayError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'RelayError';
  }
}

// API error returned by the Relay server
export class APIError extends RelayError {
  readonly statusCode: number;
  readonly code?: string;
  readonly details?: Record<string, unknown>;

  constructor(
    message: string,
    statusCode: number,
    code?: string,
    details?: Record<string, unknown>
  ) {
    super(message);
    this.name = 'APIError';
    this.statusCode = statusCode;
    this.code = code;
    this.details = details;
  }

  static fromResponse(statusCode: number, body: unknown): APIError {
    if (typeof body === 'object' && body !== null) {
      const { error, code, details } = body as Record<string, unknown>;
      return new APIError(
        typeof error === 'string' ? error : `HTTP ${statusCode}`,
        statusCode,
        typeof code === 'string' ? code : undefined,
        typeof details === 'object' ? (details as Record<string, unknown>) : undefined
      );
    }
    return new APIError(`HTTP ${statusCode}`, statusCode);
  }

  isUnauthorized(): boolean {
    return this.statusCode === 401;
  }

  isNotFound(): boolean {
    return this.statusCode === 404;
  }

  isConflict(): boolean {
    return this.statusCode === 409;
  }

  isRateLimited(): boolean {
    return this.statusCode === 429;
  }

  isBadRequest(): boolean {
    return this.statusCode === 400 || this.statusCode === 422;
  }

  isServerError(): boolean {
    return this.statusCode >= 500;
  }
}

// Signature verification errors
export class SignatureError extends RelayError {
  constructor(message: string) {
    super(message);
    this.name = 'SignatureError';
  }
}

export class InvalidSignatureError extends SignatureError {
  constructor() {
    super('Signature mismatch');
    this.name = 'InvalidSignatureError';
  }
}

export class MissingSignatureError extends SignatureError {
  constructor() {
    super('Missing signature header');
    this.name = 'MissingSignatureError';
  }
}

export class InvalidTimestampError extends SignatureError {
  constructor(message = 'Invalid or expired timestamp') {
    super(message);
    this.name = 'InvalidTimestampError';
  }
}

export class MissingTimestampError extends SignatureError {
  constructor() {
    super('Missing timestamp header');
    this.name = 'MissingTimestampError';
  }
}
