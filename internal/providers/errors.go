package providers

import (
	"errors"
	"fmt"
)

// Standard provider errors
var (
	// ErrProviderUnavailable indicates the provider service is unreachable or down
	ErrProviderUnavailable = errors.New("provider service is unavailable")
	
	// ErrInvalidInput indicates the input provided to the provider is invalid
	ErrInvalidInput = errors.New("invalid input provided to provider")
	
	// ErrTimeout indicates the provider operation timed out
	ErrTimeout = errors.New("provider operation timed out")
	
	// ErrRateLimited indicates the provider is rate limiting requests
	ErrRateLimited = errors.New("provider rate limit exceeded")
	
	// ErrUnauthorized indicates authentication/authorization failed
	ErrUnauthorized = errors.New("provider authentication failed")
	
	// ErrInternalError indicates an internal error in the provider
	ErrInternalError = errors.New("provider internal error")
)

// WrapProviderError wraps a provider-specific error with a standard error
func WrapProviderError(standardErr error, providerErr error) error {
	if providerErr == nil {
		return standardErr
	}
	return fmt.Errorf("%w: %v", standardErr, providerErr)
}