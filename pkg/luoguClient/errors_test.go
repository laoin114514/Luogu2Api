package luoguclient

import (
	"errors"
	"testing"
)

func TestAuthError(t *testing.T) {
	err := &AuthError{Code: 403, Message: "wrong password"}
	want := "auth error [403]: wrong password"
	if err.Error() != want {
		t.Errorf("expected %q, got %q", want, err.Error())
	}
}

func TestCSRFErrorUnwrap(t *testing.T) {
	inner := errors.New("timeout")
	err := &CSRFError{Err: inner}
	if !errors.Is(err, inner) {
		t.Error("CSRFError should unwrap to inner error")
	}
}

func TestNetworkErrorUnwrap(t *testing.T) {
	inner := errors.New("connection refused")
	err := &NetworkError{Err: inner}
	if !errors.Is(err, inner) {
		t.Error("NetworkError should unwrap to inner error")
	}
}

func TestUnauthorizedError(t *testing.T) {
	err := &UnauthorizedError{}
	if err.Error() == "" {
		t.Error("UnauthorizedError should have a message")
	}
}
