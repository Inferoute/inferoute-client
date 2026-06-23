package llm

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	// ErrUnreachable is returned when the local LLM process cannot be reached.
	ErrUnreachable = errors.New("llm unreachable")
	// ErrHTTP is returned when the LLM responds with a non-success HTTP status.
	ErrHTTP = errors.New("llm http error")
)

func wrapRequestErr(err error) error {
	if err == nil {
		return nil
	}
	if isConnectionErr(err) {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	return err
}

func wrapHTTPStatusErr(statusCode int) error {
	return fmt.Errorf("%w: status %d", ErrHTTP, statusCode)
}

func isConnectionErr(err error) bool {
	for err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return true
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "no such host") ||
			strings.Contains(msg, "connect:") ||
			strings.Contains(msg, "dial tcp") {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
