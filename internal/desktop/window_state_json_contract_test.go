package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveWindowStateJSONContract(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	for _, tc := range []struct {
		name        string
		input, want WindowState
	}{
		{"zero", WindowState{}, WindowState{}},
		{"normal", WindowState{1280, 800, true}, WindowState{1280, 800, true}},
		{"minimum", WindowState{1, 1, false}, WindowState{900, 600, false}},
		{"maximum_int", WindowState{maxInt, maxInt, true}, WindowState{8192, 8192, true}},
		{"minimum_int", WindowState{-maxInt - 1, -maxInt - 1, true}, WindowState{900, 600, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := withTempUserDataDir(t)
			if err := SaveWindowState(tc.input); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, windowStateFile))
			if err != nil {
				t.Fatal(err)
			}
			wantJSON := fmt.Sprintf("{\n  \"width\": %d,\n  \"height\": %d,\n  \"maximised\": %t\n}", tc.want.Width, tc.want.Height, tc.want.Maximised)
			if string(data) != wantJSON {
				t.Fatalf("saved JSON = %s; want %s", data, wantJSON)
			}
			got, err := LoadWindowState()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("loaded %+v; want %+v", got, tc.want)
			}
		})
	}
}
