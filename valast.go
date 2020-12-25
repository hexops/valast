package valast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/shurcooL/go-goon/bypass"
)

// Options describes options for the conversion process.
type Options struct {
	// Unqualify, if true, indicates that types should be unqualified. e.g.:
	//
	// 	int(8)           -> 8
	//  Bar{}            -> Bar{}
	//  string("foobar") -> "foobar"
	//
	// This is set to true automatically when operating within a struct.
	Unqualify bool

	// PackagePath, if non-zero, describes that the literal is being produced within the described
	// package path, and thus type selectors `pkg.Foo` should just be written `Foo` if the package
	// path and name match.
	PackagePath string

	// PackageName, if non-zero, describes that the literal is being produced within the described
	// package name, and thus type selectors `pkg.Foo` should just be written `Foo` if the package
	// path and name match.
	PackageName string

	// ExportedOnly indicates if only exported fields and values should be included.
	ExportedOnly bool
}

// String converts the value v into the equivalent Go literal syntax. The input must be one of
// these kinds:
//
// 	bool
// 	int, int8, int16, int32, int64
// 	uint, uint8, uint16, uint32, uint64
// 	uintptr
// 	float32, float64
// 	complex64, complex128
// 	array
// 	interface
// 	map
// 	ptr
// 	slice
// 	string
// 	struct
// 	unsafe pointer
//
func String(v reflect.Value, opt *Options) (string, error) {
	if opt == nil {
		opt = &Options{}
	}
	var buf bytes.Buffer
	ast := AST(v, opt)
	if ast == nil {
		return "", fmt.Errorf("valast: cannot convert value of kind:%s type:%T", v.Kind(), v.Interface())
	}
	if err := printer.Fprint(&buf, token.NewFileSet(), ast); err != nil {
		return "", err
	}
	// TODO: `printer.Fprint` does not produce gofmt'd output
	return buf.String(), nil
}

func fprintf(w io.Writer, format string, a ...interface{}) error {
	_, err := fmt.Fprintf(w, format, a...)
	return err
}

func basicLit(kind token.Token, typ string, v interface{}, opt *Options) ast.Expr {
	if opt.Unqualify {
		return &ast.BasicLit{Kind: kind, Value: fmt.Sprint(v)}
	}
	return &ast.CallExpr{
		Fun:  ast.NewIdent(typ),
		Args: []ast.Expr{&ast.BasicLit{Kind: kind, Value: fmt.Sprint(v)}},
	}
}

// AST is identical to String, except it returns an AST.
//
// Returns nil if the value v is not of a kind that can be converted.
func AST(v reflect.Value, opt *Options) ast.Expr {
	vv := unexported(v)
	switch vv.Kind() {
	case reflect.Bool:
		if opt.Unqualify {
			return ast.NewIdent(fmt.Sprint(v))
		}
		return &ast.CallExpr{
			Fun:  ast.NewIdent("bool"),
			Args: []ast.Expr{ast.NewIdent(fmt.Sprint(v))},
		}
	case reflect.Int:
		return basicLit(token.INT, "int", v, opt)
	case reflect.Int8:
		return basicLit(token.INT, "int8", v, opt)
	case reflect.Int16:
		return basicLit(token.INT, "int16", v, opt)
	case reflect.Int32:
		return basicLit(token.INT, "int32", v, opt)
	case reflect.Int64:
		return basicLit(token.INT, "int64", v, opt)
	case reflect.Uint:
		return basicLit(token.INT, "uint", v, opt)
	case reflect.Uint8:
		return basicLit(token.INT, "uint8", v, opt)
	case reflect.Uint16:
		return basicLit(token.INT, "uint16", v, opt)
	case reflect.Uint32:
		return basicLit(token.INT, "uint32", v, opt)
	case reflect.Uint64:
		return basicLit(token.INT, "uint64", v, opt)
	case reflect.Uintptr:
		return basicLit(token.INT, "uintptr", v, opt)
	case reflect.Float32:
		return basicLit(token.FLOAT, "float32", v, opt)
	case reflect.Float64:
		return basicLit(token.FLOAT, "float64", v, opt)
	case reflect.Complex64:
		return basicLit(token.FLOAT, "complex64", v, opt)
	case reflect.Complex128:
		return basicLit(token.FLOAT, "complex128", v, opt)
	case reflect.Array:
		// TODO: handle unexported
		var elts []ast.Expr
		for i := 0; i < vv.Len(); i++ {
			elts = append(elts, AST(vv.Index(i), opt))
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt),
			Elts: elts,
		}
	case reflect.Interface:
		if opt.ExportedOnly && !ast.IsExported(vv.Type().Name()) {
			return nil
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt),
			Elts: []ast.Expr{AST(unexported(vv.Elem()), opt)},
		}
	case reflect.Map:
		panic("TODO")
	case reflect.Ptr:
		return &ast.UnaryExpr{
			Op: token.AND,
			X:  AST(reflect.Indirect(vv), opt),
		}
	case reflect.Slice:
		// TODO: handle unexported
		var elts []ast.Expr
		for i := 0; i < vv.Len(); i++ {
			elts = append(elts, AST(vv.Index(i), opt))
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt),
			Elts: elts,
		}
	case reflect.String:
		// TODO: format long strings, strings with unicode, etc. more nicely
		return basicLit(token.STRING, "string", strconv.Quote(v.String()), opt)
	case reflect.Struct:
		if opt.ExportedOnly && !ast.IsExported(vv.Type().Name()) {
			return nil
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt),
			Elts: structValue(vv, opt),
		}
	case reflect.UnsafePointer:
		panic("TODO")
	default:
		return nil
	}
}

func typeExpr(v reflect.Type, opt *Options) ast.Expr {
	switch v.Kind() {
	case reflect.Array:
		return &ast.ArrayType{
			Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(v.Len())},
			Elt: typeExpr(v.Elem(), opt),
		}
	case reflect.Interface:
		// TODO: what if not exported?
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName := packageNameFromPath(v.PkgPath())
				if pkgName != opt.PackageName {
					return &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())}
				}
			}
			return ast.NewIdent(v.Name())
		}
		var methods []*ast.Field
		for i := 0; i < v.NumMethod(); i++ {
			method := v.Method(i)
			methods = append(methods, &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(method.Name)},
				Type:  typeExpr(method.Type, opt),
			})
		}
		return &ast.InterfaceType{Methods: &ast.FieldList{List: methods}}
	case reflect.Func:
		// Note: reflect cannot determine parameter/result names. See https://groups.google.com/g/golang-nuts/c/nM_ZhL7fuGc
		var params []*ast.Field
		for i := 0; i < v.NumIn(); i++ {
			param := v.In(i)
			params = append(params, &ast.Field{
				Type: typeExpr(param, opt),
			})
		}
		var results []*ast.Field
		for i := 0; i < v.NumOut(); i++ {
			result := v.Out(i)
			results = append(results, &ast.Field{
				Type: typeExpr(result, opt),
			})
		}
		return &ast.FuncType{
			Params:  &ast.FieldList{List: params},
			Results: &ast.FieldList{List: results},
		}
	case reflect.Map:
		panic("TODO")
	case reflect.Ptr:
		return &ast.StarExpr{X: typeExpr(v.Elem(), opt)}
	case reflect.Slice:
		return &ast.ArrayType{Elt: typeExpr(v.Elem(), opt)}
	case reflect.Struct:
		// TODO: what if not exported?
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName := packageNameFromPath(v.PkgPath())
				if pkgName != opt.PackageName {
					return &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())}
				}
			}
			return ast.NewIdent(v.Name())
		}
		var fields []*ast.Field
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fields = append(fields, &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(field.Name)},
				Type:  typeExpr(field.Type, opt),
			})
		}
		return &ast.StructType{
			Fields: &ast.FieldList{List: fields},
		}
	default:
		return ast.NewIdent(v.Name())
	}
}

// TODO: obviously not always correct
func packageNameFromPath(path string) string {
	pkgName := path
	dot := strings.LastIndexByte(path, '/')
	if dot != -1 {
		pkgName = pkgName[dot+1:]
	}
	return pkgName
}

func unexported(v reflect.Value) reflect.Value {
	if v == (reflect.Value{}) {
		return v
	}
	return bypass.UnsafeReflectValue(v)
}

func structValue(v reflect.Value, opt *Options) (elts []ast.Expr) {
	tmp := *opt
	tmp.Unqualify = true
	opt = &tmp

	var keyValueExprs []ast.Expr
	for i := 0; i < v.NumField(); i++ {
		if opt.ExportedOnly && !ast.IsExported(v.Type().Field(i).Name) {
			continue
		}
		if unexported(v.Field(i)).IsZero() {
			continue
		}
		keyValueExprs = append(keyValueExprs, &ast.KeyValueExpr{
			Key:   ast.NewIdent(v.Type().Field(i).Name),
			Value: AST(unexported(v.Field(i)), opt),
		})
	}
	return keyValueExprs
}
