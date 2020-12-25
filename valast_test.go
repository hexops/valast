package valast

import (
	"reflect"
	"testing"

	"github.com/hexops/autogold"
)

type foo struct {
	bar string
}

type baz struct {
	Bam  complex64
	zeta foo
	Beta interface{}
}

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
		{
			name:  "complex64",
			input: complex64(1.234),
		},
		{
			name:  "complex64_unqualify",
			input: complex64(1.234),
			opt:   &Options{Unqualify: true},
		},
		{
			name:  "string",
			input: string("hello \t world"),
		},
		{
			name: "string_unqualify",
			input: string(`one
two
three`),
			opt: &Options{Unqualify: true},
		},
		{
			name: "struct_anonymous",
			input: struct {
				a, b int
				V    string
			}{a: 1, b: 2, V: "efg"},
		},
		{
			name: "struct_same_package",
			input: baz{
				Bam: 1.34,
				zeta: foo{
					bar: "hello",
				},
			},
			opt: &Options{PackageName: "valast", PackagePath: "github.com/hexops/valast"},
		},
		{
			name: "struct_same_package_exported_only",
			input: baz{
				Bam: 1.34,
				zeta: foo{
					bar: "hello",
				},
			},
			err: "valast: cannot convert value of kind:struct type:valast.baz",
			opt: &Options{PackageName: "valast", PackagePath: "github.com/hexops/valast", ExportedOnly: true},
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
