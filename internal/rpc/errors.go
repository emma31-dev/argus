package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel error kinds. Use errors.Is to test an error returned by this
// package against them.
var (
	// ErrMethodNotFound means the contract has no such view method
	// (FunctionCallError / MethodResolveError / MethodNotFound).
	ErrMethodNotFound = errors.New("rpc: contract method not found")
	// ErrUnknownBlock means the provider does not know the requested block
	// (UNKNOWN_BLOCK). It also covers garbage-collected blocks.
	ErrUnknownBlock = errors.New("rpc: unknown block")
	// ErrNeedsArchival means the block was garbage collected
	// (GARBAGE_COLLECTED_BLOCK) or could not be served by any non-archival
	// provider: retry against an archival node.
	ErrNeedsArchival = errors.New("rpc: block needs an archival node")
	// ErrAccountNotFound means the account, or its contract code, does not
	// exist at the requested block.
	ErrAccountNotFound = errors.New("rpc: account or contract code not found")
	// ErrContractPanic is a deterministic contract-level failure: the view
	// call ran and panicked or otherwise failed.
	ErrContractPanic = errors.New("rpc: contract execution failed")
	// ErrInvalidRequest means the node rejected the request itself (bad
	// account id, bad parameters). Retrying elsewhere will not help.
	ErrInvalidRequest = errors.New("rpc: invalid request")
	// ErrProviderUnavailable covers transport errors, timeouts, HTTP 429/5xx
	// and retryable RPC errors. The call may succeed on another provider.
	ErrProviderUnavailable = errors.New("rpc: provider unavailable")
)

// IsProviderFailure reports whether err says the provider (rather than the
// contract call) failed, i.e. a whole read round should be retried on
// another provider.
func IsProviderFailure(err error) bool {
	return errors.Is(err, ErrProviderUnavailable) ||
		errors.Is(err, ErrUnknownBlock) ||
		errors.Is(err, ErrNeedsArchival)
}

// IsRateLimited reports whether err is an HTTP 429 from a provider. It is
// also a provider failure; callers use this to wait instead of giving up.
func IsRateLimited(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "HTTP_429"
}

// Error is a classified failure of one request to one provider.
type Error struct {
	Provider string
	Method   string // JSON-RPC method, e.g. "query"
	Kind     error  // one of the Err* sentinels
	Cause    error  // underlying transport error, may be nil
	Code     string // NEAR error cause name, e.g. "UNKNOWN_BLOCK", may be empty
	Message  string // server-provided detail
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v (provider %s, method %s", e.Kind, e.Provider, e.Method)
	if e.Code != "" {
		fmt.Fprintf(&b, ", code %s", e.Code)
	}
	b.WriteString(")")
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	if e.Cause != nil {
		b.WriteString(": " + e.Cause.Error())
	}
	return b.String()
}

// Unwrap exposes the kind and the cause to errors.Is / errors.As.
func (e *Error) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Kind}
	}
	return []error{e.Kind, e.Cause}
}

// rpcError is the JSON-RPC "error" object NEAR returns.
type rpcError struct {
	Name    string          `json:"name"`
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Cause   struct {
		Name string          `json:"name"`
		Info json.RawMessage `json:"info"`
	} `json:"cause"`
}

// detail flattens the human-readable parts of the error.
func (e *rpcError) detail() string {
	parts := []string{}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	var s string
	if len(e.Data) > 0 && json.Unmarshal(e.Data, &s) == nil && s != "" {
		parts = append(parts, s)
	}
	var info struct {
		ErrorMessage string `json:"error_message"`
	}
	if len(e.Cause.Info) > 0 && json.Unmarshal(e.Cause.Info, &info) == nil && info.ErrorMessage != "" {
		parts = append(parts, info.ErrorMessage)
	}
	return strings.Join(parts, ": ")
}

// classifyRPCError maps a JSON-RPC error object to a kind and cause name.
func classifyRPCError(e *rpcError) (kind error, code string) {
	code = e.Cause.Name
	detail := e.detail()
	switch code {
	case "UNKNOWN_BLOCK":
		return ErrUnknownBlock, code
	case "GARBAGE_COLLECTED_BLOCK":
		return fmt.Errorf("%w: %w", ErrUnknownBlock, ErrNeedsArchival), code
	case "UNKNOWN_ACCOUNT", "NO_CONTRACT_CODE":
		return ErrAccountNotFound, code
	case "INVALID_ACCOUNT":
		return ErrInvalidRequest, code
	case "CONTRACT_EXECUTION_ERROR":
		k := classifyMessage(detail)
		if errors.Is(k, ErrProviderUnavailable) {
			// An execution error that is not recognisably transient is
			// still a contract-level failure.
			k = ErrContractPanic
		}
		return k, code
	case "":
		// No structured cause: fall back to the name and message.
		if e.Name == "REQUEST_VALIDATION_ERROR" {
			return ErrInvalidRequest, e.Name
		}
		return classifyMessage(e.Message + " " + detail), e.Name
	default:
		// NOT_SYNCED_YET, UNAVAILABLE_SHARD, TIMEOUT_ERROR, INTERNAL_ERROR...
		return ErrProviderUnavailable, code
	}
}

// classifyMessage maps a free-text failure (the "error" string NEAR puts in
// a call_function result with HTTP 200) to a kind. Unrecognised text is
// treated as a provider problem so that fallback is attempted.
func classifyMessage(msg string) error {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "methodnotfound") || strings.Contains(m, "method not found"):
		return ErrMethodNotFound
	case strings.Contains(m, "garbage"):
		return fmt.Errorf("%w: %w", ErrUnknownBlock, ErrNeedsArchival)
	case strings.Contains(m, "unknown block") || strings.Contains(m, "unknown_block") ||
		strings.Contains(m, "db not found error: block"):
		return ErrUnknownBlock
	case strings.Contains(m, "does not exist while viewing") ||
		strings.Contains(m, "unknownaccount") ||
		strings.Contains(m, "codedoesnotexist") ||
		strings.Contains(m, "contract code") ||
		strings.Contains(m, "no contract"):
		return ErrAccountNotFound
	case strings.Contains(m, "does not track") || strings.Contains(m, "doesn't track") ||
		strings.Contains(m, "not synced") || strings.Contains(m, "syncing"):
		return ErrProviderUnavailable
	case strings.Contains(m, "wasm execution failed") || strings.Contains(m, "functioncallerror") ||
		strings.Contains(m, "panic"):
		return ErrContractPanic
	default:
		return ErrProviderUnavailable
	}
}
