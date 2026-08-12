package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsKnownInvalidDMMActressThumbnail(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "missing image extension", url: "https://pics.dmm.co.jp/mono/actjpgs/iseya_takami", want: true},
		{name: "AWS missing image extension", url: "https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami?width=125", want: true},
		{name: "DMM placeholder", url: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg", want: true},
		{name: "valid DMM actress image", url: "https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg", want: false},
		{name: "custom extensionless image", url: "https://example.com/mono/actjpgs/custom", want: false},
		{name: "unrelated DMM extensionless URL", url: "https://www.dmm.co.jp/mono/actjpgs/custom", want: false},
		{name: "other DMM placeholder", url: "https://pics.dmm.co.jp/mono/noimage/other.jpg", want: false},
		{name: "blank", url: "", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, IsKnownInvalidDMMActressThumbnail(test.url))
		})
	}
}
