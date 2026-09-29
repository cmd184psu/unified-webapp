package golane

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type echoPayload struct {
	Msg string `json:"msg"`
}

func TestNewKind_StrictDecodeVersionAndValidate(t *testing.T) {
	validateCalls := 0
	k := NewKind[echoPayload]("acme-module.echo", 1, func(p echoPayload) error {
		validateCalls++
		if p.Msg == "" {
			return errors.New("msg is required")
		}
		return nil
	}, func(ctx context.Context, rc RunContext, p echoPayload) (any, error) {
		return p.Msg, nil
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		_, err := k.Decode(1, json.RawMessage(`{"msg":"hi","extra":true}`))
		require.Error(t, err)
	})

	t.Run("wrong version rejected", func(t *testing.T) {
		_, err := k.Decode(2, json.RawMessage(`{"msg":"hi"}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported payload version")
	})

	t.Run("validate error surfaces", func(t *testing.T) {
		_, err := k.Decode(1, json.RawMessage(`{"msg":""}`))
		require.Error(t, err)
		require.Contains(t, err.Error(), "msg is required")
	})

	t.Run("valid payload decodes and runs", func(t *testing.T) {
		p, err := k.Decode(1, json.RawMessage(`{"msg":"hi"}`))
		require.NoError(t, err)
		res, err := k.Run(context.Background(), nil, p)
		require.NoError(t, err)
		require.Equal(t, "hi", res)
	})
}
