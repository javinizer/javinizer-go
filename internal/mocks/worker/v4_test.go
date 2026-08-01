package workermocks

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMockControlledJobV4(t *testing.T) {
	m := NewMockControlledJob(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestMockEditableJobV4(t *testing.T) {
	m := NewMockEditableJob(t)
	require.NotNil(t, m)
	_ = m.EXPECT()
}

func TestPosterCropMocksAcceptBounds(t *testing.T) {
	bounds := &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 400}

	t.Run("batch job", func(t *testing.T) {
		m := NewMockBatchJobInterface(t)
		called := false
		m.EXPECT().UpdatePosterCrop("MOV-1", "crop-1", bounds).
			Run(func(movieID, croppedURL string, got *models.CropBounds) {
				called = true
				require.Equal(t, bounds, got)
			}).Return(nil)
		require.NoError(t, m.UpdatePosterCrop("MOV-1", "crop-1", bounds))
		require.True(t, called)

		m.EXPECT().UpdatePosterCrop("MOV-2", "crop-2", bounds).
			RunAndReturn(func(string, string, *models.CropBounds) error { return nil })
		require.NoError(t, m.UpdatePosterCrop("MOV-2", "crop-2", bounds))
	})

	t.Run("editable job", func(t *testing.T) {
		m := NewMockEditableJob(t)
		called := false
		m.EXPECT().UpdatePosterCrop("MOV-1", "crop-1", bounds).
			Run(func(movieID, croppedURL string, got *models.CropBounds) {
				called = true
				require.Equal(t, bounds, got)
			}).Return(nil)
		require.NoError(t, m.UpdatePosterCrop("MOV-1", "crop-1", bounds))
		require.True(t, called)

		m.EXPECT().UpdatePosterCrop("MOV-2", "crop-2", bounds).
			RunAndReturn(func(string, string, *models.CropBounds) error { return nil })
		require.NoError(t, m.UpdatePosterCrop("MOV-2", "crop-2", bounds))
	})
}
