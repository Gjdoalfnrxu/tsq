package testutil_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Gjdoalfnrxu/tsq/internal/testutil"
)

func TestEqual(t *testing.T) {
	testutil.Equal(t, 1, 1)
	testutil.Equal(t, "foo", "foo")
}

func TestErrorIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	testutil.ErrorIs(t, sentinel, sentinel)
	testutil.ErrorIs(t, fmt.Errorf("wrap: %w", sentinel), sentinel)
}

func TestNilError(t *testing.T) {
	testutil.NilError(t, nil)
}

func TestContains(t *testing.T) {
	testutil.Contains(t, "hello world", "world")
}
