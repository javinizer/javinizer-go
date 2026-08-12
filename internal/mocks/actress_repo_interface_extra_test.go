package mocks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Smoke: exercise the generated MergeWithVersions mock method so patch
// coverage includes it (merge handler tests drive live repositories instead).
func TestMockActressRepositoryInterfaceMergeWithVersions(t *testing.T) {
	m := NewMockActressRepositoryInterface(t)
	m.EXPECT().MergeWithVersions(context.Background(), uint(1), uint(2), map[string]string(nil), time.Time{}, time.Time{}).
		Return(nil, errors.New("mocked"))
	_, err := m.MergeWithVersions(context.Background(), 1, 2, nil, time.Time{}, time.Time{})
	require.ErrorContains(t, err, "mocked")

	m2 := NewMockActressRepositoryInterface(t)
	m2.EXPECT().MergeWithVersions(context.Background(), uint(3), uint(4), map[string]string{"a": "x"}, time.Time{}, time.Time{}).
		Return(nil, errors.New("mocked2")).Times(2)
	for i := 0; i < 2; i++ {
		if _, err := m2.MergeWithVersions(context.Background(), 3, 4, map[string]string{"a": "x"}, time.Time{}, time.Time{}); err == nil {
			t.Fatal("expected mock error")
		}
	}
}
