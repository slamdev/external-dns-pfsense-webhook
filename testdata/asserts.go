package testdata

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func RequireEqualStruct(t require.TestingT, expected any, actual any, msgAndArgs ...any) {
	if AssertEqualStruct(t, expected, actual, msgAndArgs...) {
		return
	}
	t.FailNow()
}

func AssertEqualStruct(t require.TestingT, expected any, actual any, msgAndArgs ...any) bool {
	expectedJson, _ := json.Marshal(expected)
	actualJson, _ := json.Marshal(actual)

	var expectedMap map[string]any
	_ = json.Unmarshal(expectedJson, &expectedMap)

	var actualMap map[string]any
	_ = json.Unmarshal(actualJson, &actualMap)

	if !cmp.Equal(expectedMap, actualMap) {
		diff := cmp.Diff(expectedMap, actualMap)
		return assert.Fail(t, fmt.Sprintf("Not equal: \n"+
			"expected: %#v\n"+
			"actual  : %#v\n%s", expectedMap, actualMap, diff), msgAndArgs...)
	}
	return true
}

func WaitFor[T any](t *testing.T, f func(c *assert.CollectT) (T, error)) T {
	var res T
	var failFastError error
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, failFastError = f(c)
	}, 4*time.Minute, 1*time.Second, "WaitFor method timed out after 4 minutes of retries")
	require.NoErrorf(t, failFastError, "WaitFor method failed with error: %v", failFastError)
	return res
}
