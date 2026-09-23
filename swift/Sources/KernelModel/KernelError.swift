/// Contract error codes (Contract/spec/errors.md). Only the code is contract.
public enum KernelError: String, Error, Codable, Sendable {
    case invalidArgument = "ERROR_CODE_INVALID_ARGUMENT"
    case notFound = "ERROR_CODE_NOT_FOUND"
    case conflict = "ERROR_CODE_CONFLICT"
    case idempotencyConflict = "ERROR_CODE_IDEMPOTENCY_CONFLICT"
    case redirectCycle = "ERROR_CODE_REDIRECT_CYCLE"
    case invalidReference = "ERROR_CODE_INVALID_REFERENCE"
    case unknownSchema = "ERROR_CODE_UNKNOWN_SCHEMA"
    case policyDenied = "ERROR_CODE_POLICY_DENIED"
}
