package models

import "testing"

func TestSplitFullNameWhitespaceAndByteContract(t *testing.T) {
	for _, tc := range []struct{ name, input, first, last string }{
		{"empty", "", "", ""},
		{"ascii whitespace", " \t\r\n\v\f", "", ""},
		{"unicode whitespace", "\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000", "", ""},
		{"one", "\u3000花子\u00a0", "花子", ""},
		{"two", " Hana\tExample ", "Hana", "Example"},
		{"many", "\u2003Mary\u00a0Jane\nvan\tExample\u3000", "Mary", "Jane van Example"},
		{"zero width is not whitespace", " \u200b\ufeff ", "\u200b\ufeff", ""},
		{"zero width inside name", "Hana\u200bExample", "Hana\u200bExample", ""},
		{"invalid UTF8 retained", " \xffA\u3000B\xfe C ", "\xffA", "B\xfe C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, last := SplitFullName(tc.input)
			if first != tc.first || last != tc.last {
				t.Fatalf("SplitFullName(%q) = (%q, %q), want (%q, %q)", tc.input, first, last, tc.first, tc.last)
			}
		})
	}
}
