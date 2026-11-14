package worker

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func TestOrganizeMetadataPersistedContract(t *testing.T) {
	empty := `{"operation_mode":"organize","steps":{"Organized":false,"Merged":false,"DisplayTitle":false,"Downloaded":false,"NFOGenerated":false,"PosterVerified":false}}`
	tests := []struct {
		name   string
		result *workflow.ApplyResult
		want   string
	}{
		{"nil result", nil, `{"operation_mode":"organize","steps":{}}`},
		{"zero result", &workflow.ApplyResult{}, empty},
		{"nil warnings", &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{}}, empty},
		{"empty warnings", &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{Warnings: []string{}}}, empty},
		{"warnings order", &workflow.ApplyResult{VideoOutcome: "copied", RecoveryWarning: "cleanup", OrganizeResult: &organizer.OrganizeResult{Warnings: []string{"second", "first", "second", ""}}}, `{"operation_mode":"organize","recovery_warning":"cleanup","steps":{"Organized":false,"Merged":false,"DisplayTitle":false,"Downloaded":false,"NFOGenerated":false,"PosterVerified":false},"video_outcome":"copied","warnings":["second","first","second",""]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizeMetadata("organize", tc.result); got != tc.want {
				t.Fatalf("got %s; want %s", got, tc.want)
			}
		})
	}
}

func TestOrganizeMetadataStepSchemaContract(t *testing.T) {
	result := &workflow.ApplyResult{}
	typ := reflect.TypeOf(result.Steps)
	for _, iface := range []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()} {
		if typ.Implements(iface) || reflect.PointerTo(typ).Implements(iface) {
			t.Fatalf("steps acquired custom encoding: %v", iface)
		}
	}
	names := []string{"Organized", "Merged", "DisplayTitle", "Downloaded", "NFOGenerated", "PosterVerified"}
	if typ.NumField() != len(names) {
		t.Fatalf("steps schema changed: %v", typ)
	}
	for i, name := range names {
		f := typ.Field(i)
		if f.Name != name || f.Type.Kind() != reflect.Bool || f.Tag != "" || f.PkgPath != "" {
			t.Fatalf("steps field changed: %+v", f)
		}
	}
	for bits := 0; bits < 1<<len(names); bits++ {
		value := reflect.ValueOf(&result.Steps).Elem()
		want := "{\"operation_mode\":\"organize\",\"steps\":{"
		for i, name := range names {
			on := bits&(1<<i) != 0
			value.Field(i).SetBool(on)
			if i > 0 {
				want += ","
			}
			want += fmt.Sprintf("%q:%t", name, on)
		}
		want += "}}"
		if got := organizeMetadata("organize", result); got != want {
			t.Fatalf("bits %d: got %s; want %s", bits, got, want)
		}
	}
}

func TestOrganizeMetadataStringEncodingContract(t *testing.T) {
	for _, s := range []string{"", "\"\\\n\t\x00", "<>&", "日本語\u2028\u2029", string([]byte{0xff, 'a', 0xc0}), "null"} {
		result := &workflow.ApplyResult{VideoOutcome: s, RecoveryWarning: s, OrganizeResult: &organizer.OrganizeResult{Warnings: []string{s, s}}}
		steps, err := json.Marshal(result.Steps)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]any{"operation_mode": s, "steps": json.RawMessage(steps), "warnings": []string{s, s}}
		if s != "" {
			expected["video_outcome"] = s
			expected["recovery_warning"] = s
		}
		want, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		got := organizeMetadata(s, result)
		if got != string(want) || !json.Valid([]byte(got)) {
			t.Fatalf("input %q: got %q; want %q", s, got, want)
		}
	}
}
