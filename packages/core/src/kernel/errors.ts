/** Error types shared by every unit. Each carries a stable `code` for API responses. */
export class PraxiumError extends Error {
  readonly code: string = "praxium_error";
  /** HTTP status the server should use when this error reaches a client. */
  readonly status: number = 500;

  constructor(message: string) {
    super(message);
    this.name = new.target.name;
  }
}

/** An argument or stored value breaks a rule (bad id, out-of-range number, ...). */
export class InvalidValueError extends PraxiumError {
  override readonly code = "invalid_value";
  override readonly status = 400;
}

/** A referenced entity, model, scenario or experiment does not exist. */
export class NotFoundError extends PraxiumError {
  override readonly code = "not_found";
  override readonly status = 404;
}

/** The request is valid but conflicts with current state (duplicate id, wrong lifecycle state). */
export class ConflictError extends PraxiumError {
  override readonly code = "conflict";
  override readonly status = 409;
}

/** The operation is not supported by this profile, model, source or capability set. */
export class UnsupportedError extends PraxiumError {
  override readonly code = "unsupported";
  override readonly status = 422;
}

/** Something tried to write to reality. Praxium only ever reads real systems (REQ-011). */
export class ReadOnlyViolationError extends PraxiumError {
  override readonly code = "read_only_violation";
  override readonly status = 403;
}

/** Normalize anything thrown into a PraxiumError (unknown errors keep their message). */
export function asPraxiumError(error: unknown): PraxiumError {
  if (error instanceof PraxiumError) return error;
  return new PraxiumError(error instanceof Error ? error.message : String(error));
}
