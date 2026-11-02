package dmm

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/stretchr/testify/require"
)

// cookieProtocolExecutor stands at CDP's supported transport boundary. It does
// not substitute the cookie action or the browser fetch implementation.
type cookieProtocolExecutor struct {
	commands []network.SetCookieParams
	failAt   int
	failure  error
}

func (e *cookieProtocolExecutor) Execute(_ context.Context, method string, params, result any) error {
	if method != network.CommandSetCookie {
		return fmt.Errorf("unexpected protocol method %q", method)
	}
	cookie, ok := params.(*network.SetCookieParams)
	if !ok {
		return fmt.Errorf("unexpected protocol parameters %T", params)
	}
	if result != nil {
		return fmt.Errorf("unexpected protocol result %T", result)
	}
	e.commands = append(e.commands, *cookie)
	if len(e.commands) == e.failAt {
		return e.failure
	}
	return nil
}

func TestAgeVerificationCookieProtocolContract(t *testing.T) {
	firstFailure := errors.New("age-cookie transport disconnected")
	secondFailure := errors.New("locale-cookie transport disconnected")
	for _, tc := range []struct {
		name      string
		failAt    int
		failure   error
		wantError string
		wantCount int
	}{
		{name: "success", wantCount: 2},
		{name: "first command fails", failAt: 1, failure: firstFailure, wantError: "failed to set age_check_done cookie: age-cookie transport disconnected", wantCount: 1},
		{name: "second command fails", failAt: 2, failure: secondFailure, wantError: "failed to set cklg cookie: locale-cookie transport disconnected", wantCount: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor := &cookieProtocolExecutor{failAt: tc.failAt, failure: tc.failure}
			err := setAgeVerificationCookies(cdp.WithExecutor(context.Background(), executor))
			if tc.failure == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.failure)
				require.EqualError(t, err, tc.wantError)
			}
			require.Len(t, executor.commands, tc.wantCount, "no later cookie command may follow the first failure")
			require.Equal(t, network.SetCookieParams{Name: "age_check_done", Value: "1", Domain: ".dmm.co.jp"}, executor.commands[0])
			if tc.wantCount == 2 {
				require.Equal(t, network.SetCookieParams{Name: "cklg", Value: "ja", Domain: ".dmm.co.jp"}, executor.commands[1])
			}
		})
	}
}
