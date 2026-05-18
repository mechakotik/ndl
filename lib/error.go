package ndl

import (
	"fmt"
	"strings"
)

// Error represents an NDL error, containing a span, a message,
// and an optional underlying error.
type Error struct {
	Span    Span
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Span.Start.Line != 0 {
		return fmt.Sprintf("%d:%d: %s", e.Span.Start.Line, e.Span.Start.Column, e.Message)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

// ErrorList is a list of NDL errors.
type ErrorList []*Error

func (l ErrorList) Error() string {
	builder := strings.Builder{}
	for idx, e := range l {
		builder.WriteString(e.Error())
		if idx < len(l)-1 {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func (l ErrorList) Err() error {
	if len(l) == 0 {
		return nil
	}
	return l
}

func (l ErrorList) Unwrap() []error {
	res := []error{}
	for _, err := range l {
		if err != nil {
			res = append(res, err)
		}
	}
	return res
}

func (l *ErrorList) Add(err error) {
	if err == nil {
		return
	}

	switch concr := err.(type) {
	case *Error:
		if concr != nil {
			*l = append(*l, concr)
		}

	case ErrorList:
		for _, nested := range concr {
			l.Add(nested)
		}

	case *ErrorList:
		for _, nested := range *concr {
			l.Add(nested)
		}

	default:
		*l = append(*l, &Error{
			Span:    Span{},
			Message: err.Error(),
			Err:     err,
		})
	}
}

func makeError(span Span, format string, args ...any) error {
	return ErrorList{&Error{
		Span:    span,
		Message: fmt.Sprintf(format, args...),
	}}.Err()
}

func wrapError(span Span, err error, format string, args ...any) error {
	return ErrorList{&Error{
		Span:    span,
		Message: fmt.Sprintf(format, args...),
		Err:     err,
	}}.Err()
}

func mergeErrors(errs ...error) error {
	res := ErrorList{}
	for _, err := range errs {
		res.Add(err)
	}
	return res.Err()
}
