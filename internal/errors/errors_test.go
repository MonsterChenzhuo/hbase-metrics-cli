package errors

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorf_FormatsWithCode(t *testing.T) {
	err := Errorf(CodeFlagInvalid, "bad role %q", "foo")
	require.Equal(t, "bad role \"foo\"", err.Error())

	var ce *CodedError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, CodeFlagInvalid, ce.Code)
	require.Equal(t, ExitUserError, ce.ExitCode())
}

func TestWithHint_AttachesHintAndPreservesCode(t *testing.T) {
	err := WithHint(Errorf(CodeVMHTTP4XX, "401"), "set HBASE_VM_USER")
	var ce *CodedError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "set HBASE_VM_USER", ce.Hint)
	require.Equal(t, CodeVMHTTP4XX, ce.Code)
}

func TestWriteJSON_EmitsEnvelope(t *testing.T) {
	err := WithHint(Errorf(CodeVMHTTP5XX, "VictoriaMetrics returned 502"), "retry")
	var buf bytes.Buffer
	WriteJSON(&buf, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, "VM_HTTP_5XX", got["error"]["code"])
	require.Equal(t, "VictoriaMetrics returned 502", got["error"]["message"])
	require.Equal(t, "retry", got["error"]["hint"])
}

func TestExitCode_DefaultsToOneForUnknownErrors(t *testing.T) {
	require.Equal(t, ExitInternal, ExitCode(errors.New("plain")))
}

func TestExitCode_ZeroForNoData(t *testing.T) {
	err := Errorf(CodeNoData, "empty result")
	require.Equal(t, 0, ExitCode(err))
}
