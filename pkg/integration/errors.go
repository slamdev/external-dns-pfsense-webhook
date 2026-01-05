package integration

import (
	"errors"
	"fmt"
	"runtime/debug"
)

type ValidationError struct {
	err string
}

func (e *ValidationError) Error() string {
	return e.err
}

func NewValidationError(err string) *ValidationError {
	return &ValidationError{err: err}
}

func IsValidationError(err error) bool {
	var base *ValidationError
	return errors.As(err, &base)
}

type MissingEntityError struct {
	err string
}

func (e *MissingEntityError) Error() string {
	return e.err
}

func NewMissingEntityError(err string) *MissingEntityError {
	return &MissingEntityError{err: err}
}

func IsMissingEntityError(err error) bool {
	var base *MissingEntityError
	return errors.As(err, &base)
}

type ResourceConflictError struct {
	err string
}

func (e *ResourceConflictError) Error() string {
	return e.err
}

func NewResourceConflictError(err string) *ResourceConflictError {
	return &ResourceConflictError{err: err}
}

func IsResourceConflictError(err error) bool {
	var base *ResourceConflictError
	return errors.As(err, &base)
}

type AccessDeniedError struct {
	err string
}

func (e *AccessDeniedError) Error() string {
	return e.err
}

func NewAccessDeniedError(err string) *AccessDeniedError {
	return &AccessDeniedError{err: err}
}

func IsAccessDeniedError(err error) bool {
	var base *AccessDeniedError
	return errors.As(err, &base)
}

func CatchPanic(f func() error) (err error) {
	defer func() {
		rec := recover()
		if rec != nil {
			err = fmt.Errorf("panic: %+v\n%s", rec, string(debug.Stack()))
		}
	}()
	return f()
}
