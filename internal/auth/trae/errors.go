package trae

import "errors"

var (
	ErrPollingTimeout      = errors.New("trae: polling timeout, user did not authorize in time")
	ErrAccessDenied        = errors.New("trae: access denied by user")
	ErrTokenFetchFailed    = errors.New("trae: failed to fetch token from server")
	ErrRefreshTokenInvalid = errors.New("trae: refresh token is invalid or expired")
)

func GetUserFriendlyMessage(err error) string {
	switch {
	case errors.Is(err, ErrPollingTimeout):
		return "Authentication timed out. Please try again."
	case errors.Is(err, ErrAccessDenied):
		return "Access denied. Please try again and approve the login request."
	case errors.Is(err, ErrRefreshTokenInvalid):
		return "Refresh token is invalid or expired. Please log in again."
	case errors.Is(err, ErrTokenFetchFailed):
		return "Failed to fetch token from server. Please try again."
	default:
		return "Authentication failed: " + err.Error()
	}
}
