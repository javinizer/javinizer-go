package contracts

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/assert"
)

func TestBatchScrapeRequest_OperationMode(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantMode string
	}{
		{
			name:     "empty operation_mode defaults to empty",
			json:     `{"files":["/test/file.mp4"]}`,
			wantMode: "",
		},
		{
			name:     "organize operation_mode",
			json:     `{"files":["/test/file.mp4"],"operation_mode":"organize"}`,
			wantMode: "organize",
		},
		{
			name:     "in-place operation_mode",
			json:     `{"files":["/test/file.mp4"],"operation_mode":"in-place"}`,
			wantMode: "in-place",
		},
		{
			name:     "metadata-artwork operation_mode",
			json:     `{"files":["/test/file.mp4"],"operation_mode":"metadata-artwork"}`,
			wantMode: "metadata-artwork",
		},
		{
			name:     "preview operation_mode",
			json:     `{"files":["/test/file.mp4"],"operation_mode":"preview"}`,
			wantMode: "preview",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req BatchScrapeRequest
			err := json.Unmarshal([]byte(tt.json), &req)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantMode, req.OperationMode)
		})
	}
}

func TestOrganizeRequest_OperationMode(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantMode string
	}{
		{
			name:     "no operation_mode",
			json:     `{"destination":"/output"}`,
			wantMode: "",
		},
		{
			name:     "organize operation_mode",
			json:     `{"destination":"/output","operation_mode":"organize"}`,
			wantMode: "organize",
		},
		{
			name:     "in-place operation_mode",
			json:     `{"destination":"/output","operation_mode":"in-place"}`,
			wantMode: "in-place",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req OrganizeRequest
			err := json.Unmarshal([]byte(tt.json), &req)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantMode, req.OperationMode)
		})
	}
}

func TestOrganizePreviewRequest_OperationMode(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantMode string
	}{
		{
			name:     "no operation_mode",
			json:     `{"destination":"/output"}`,
			wantMode: "",
		},
		{
			name:     "preview operation_mode",
			json:     `{"destination":"/output","operation_mode":"preview"}`,
			wantMode: "preview",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req OrganizePreviewRequest
			err := json.Unmarshal([]byte(tt.json), &req)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantMode, req.OperationMode)
		})
	}
}

func TestBatchScrapeRequest_ManualInputsOmittedWhenEmpty(t *testing.T) {
	var req BatchScrapeRequest
	assert.NoError(t, json.Unmarshal([]byte(`{"files":["/test/a.mp4"]}`), &req))
	assert.Nil(t, req.ManualInputs)

	req = BatchScrapeRequest{Files: []string{"/test/a.mp4"}}
	data, err := json.Marshal(req)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "manual_inputs")

	req = BatchScrapeRequest{Files: []string{"/test/a.mp4"}, ManualInputs: map[string]string{}}
	data, err = json.Marshal(req)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "manual_inputs")
}

func TestBatchScrapeRequest_ManualInputsRoundTrip(t *testing.T) {
	req := BatchScrapeRequest{
		Files:        []string{"/test/a.mp4", "/test/b.mp4"},
		ManualInputs: map[string]string{"/test/a.mp4": "IPX-123", "/test/b.mp4": "https://example.com/v/456"},
	}
	data, err := json.Marshal(req)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"manual_inputs":`)
	var got BatchScrapeRequest
	assert.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, req.ManualInputs, got.ManualInputs)
}

func TestOrganizePreviewResponse_OperationMode(t *testing.T) {
	resp := OrganizePreviewResponse{
		FolderName:    "TEST-001",
		FileName:      "TEST-001",
		FullPath:      "/output/TEST-001/TEST-001.mp4",
		OperationMode: operationmode.OperationModeOrganize,
	}
	assert.Equal(t, operationmode.OperationModeOrganize, resp.OperationMode)

	data, err := json.Marshal(resp)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"operation_mode":"organize"`)
}
