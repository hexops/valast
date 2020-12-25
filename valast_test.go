package valast

import (
	"reflect"
	"testing"

	"github.com/hexops/autogold"
)

func TestString(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		opt   *Options
		err   string
	}{
		{
			name:  "bool",
			input: true,
		},
		{
			name:  "bool_unqualify",
			input: false,
			opt:   &Options{Unqualify: true},
		},
		{
			name:  "int32",
			input: int32(1234),
		},
		{
			name:  "int32_unqualify",
			input: int32(1234),
			opt:   &Options{Unqualify: true},
		},
		{
			name:  "uintptr",
			input: uintptr(1234),
		},
		{
			name:  "uintptr_unqualify",
			input: uintptr(1234),
			opt:   &Options{Unqualify: true},
		},
		{
			name:  "float64",
			input: float64(1.234),
		},
		{
			name:  "float64_unqualify",
			input: float64(1.234),
			opt:   &Options{Unqualify: true},
		},
	}
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got, err := String(reflect.ValueOf(tst.input), tst.opt)
			if tst.err != "" && tst.err != err.Error() {
				t.Fatal("\ngot:\n", err, "\nwant:\n", tst.err)
				return
			}
			autogold.Equal(t, got)
		})
	}
}
