package errors

import (
	stdErrors "errors"
)

func Is(err error, target error) bool {
	return stdErrors.Is(err, target)
}

func As(err error, target interface{}) bool {
	return stdErrors.As(err, target)
}

func New(text string) error {
	return stdErrors.New(text)
}
