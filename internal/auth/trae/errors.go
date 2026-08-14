// Package trae provides authentication and token management for ByteDance's Trae IDE.
package trae

// AuthError represents a Trae authentication error.
type AuthError struct {
	Code    string
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

// Common error codes.
const (
	ErrOAuthFailed     = "oauth_failed"
	ErrTokenExpired    = "token_expired"
	ErrRefreshFailed   = "refresh_failed"
	ErrUnauthorized    = "unauthorized"
	ErrNetwork         = "network_error"
	ErrInvalidResponse = "invalid_response"
)

// NewAuthError creates a new authentication error.
func NewAuthError(code, message string) *AuthError {
	return &AuthError{Code: code, Message: message}
}
