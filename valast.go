package valast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"reflect"
	"strconv"

	"github.com/shurcooL/go-goon/bypass"
	"golang.org/x/tools/go/packages"
)

// Options describes options for the conversion process.
type Options struct {
	// Unqualify, if true, indicates that types should be unqualified. e.g.:
	//
	// 	int(8)           -> 8
	//  Bar{}            -> Bar{}
	//  string("foobar") -> "foobar"
	//
	// This is set to true automatically when operating within a context where type qualification
	// is definitively not needed, e.g. when producing values for a struct or map.
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

	// PackagePathToName, if non-nil, is called to convert a Go package path to the package name
	// written in its source. The default is DefaultPackagePathToName
	PackagePathToName func(path string) (string, error)
}

func (o *Options) withUnqualify() *Options {
	tmp := *o
	tmp.Unqualify = true
	return &tmp
}

func (o *Options) packagePathToName(path string) (string, error) {
	if o.PackagePathToName != nil {
		return o.PackagePathToName(path)
	}
	return DefaultPackagePathToName(path)
}

// DefaultPackagePathToName loads the specified package from disk to determine the package name.
func DefaultPackagePathToName(path string) (string, error) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName}, path)
	if err != nil {
		return "", err
	}
	return pkgs[0].Name, nil
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
		var typ = "nil"
		if v != (reflect.Value{}) {
			typ = fmt.Sprintf("%T", v.Interface())
		}
		return "", fmt.Errorf("valast: cannot convert value of kind:%s type:%s", v.Kind(), typ)
	}
	if err := format.Node(&buf, token.NewFileSet(), ast); err != nil {
		return "", err
	}
	return buf.String(), nil
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
	if v == (reflect.Value{}) {
		// Technically this is an invalid reflect.Value, but we handle it to be gracious in the
		// case of:
		//
		//  var x interface{}
		// 	valast.AST(reflect.ValueOf(x))
		//
		return ast.NewIdent("nil")
	}

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
		if opt.Unqualify {
			return AST(unexported(vv.Elem()), opt.withUnqualify())
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt),
			Elts: []ast.Expr{AST(unexported(vv.Elem()), opt.withUnqualify())},
		}
	case reflect.Map:
		// TODO: what if not exported?
		var keyValueExprs []ast.Expr
		keys := vv.MapKeys()
		for _, key := range keys {
			value := vv.MapIndex(key)
			keyValueExprs = append(keyValueExprs, &ast.KeyValueExpr{
				Key:   AST(key, opt.withUnqualify()),
				Value: AST(value, opt.withUnqualify()),
			})
		}
		return &ast.CompositeLit{
			Type: typeExpr(vv.Type(), opt.withUnqualify()),
			Elts: keyValueExprs,
		}
	case reflect.Ptr:
		opt.Unqualify = false
		if vv.Elem().Kind() == reflect.Interface {
			// Pointer to interface; cannot be created in a single expression.
			return nil
		}
		return &ast.UnaryExpr{
			Op: token.AND,
			X:  AST(vv.Elem(), opt),
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
			Elts: structValue(vv, opt.withUnqualify()),
		}
	case reflect.UnsafePointer:
		return &ast.CallExpr{
			Fun: &ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent("Pointer")},
			Args: []ast.Expr{
				&ast.CallExpr{
					Fun:  ast.NewIdent("uintptr"),
					Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("0x%x", v.Pointer())}},
				},
			},
		}
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
				// TODO: bubble up errors
				pkgName, _ := opt.packagePathToName(v.PkgPath())
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
		// TODO: what if not exported?
		return &ast.MapType{
			Key:   typeExpr(v.Key(), opt),
			Value: typeExpr(v.Elem(), opt),
		}
	case reflect.Ptr:
		return &ast.StarExpr{X: typeExpr(v.Elem(), opt)}
	case reflect.Slice:
		return &ast.ArrayType{Elt: typeExpr(v.Elem(), opt)}
	case reflect.Struct:
		// TODO: what if not exported?
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				// TODO: bubble up errors
				pkgName, _ := opt.packagePathToName(v.PkgPath())
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

func unexported(v reflect.Value) reflect.Value {
	if v == (reflect.Value{}) {
		return v
	}
	return bypass.UnsafeReflectValue(v)
}

func structValue(v reflect.Value, opt *Options) (elts []ast.Expr) {
	var keyValueExprs []ast.Expr
	for i := 0; i < v.NumField(); i++ {
		if opt.ExportedOnly && !ast.IsExported(v.Type().Field(i).Name) {
			continue
		}
		if unexported(v.Field(i)).IsZero() {
			continue
		}
		value := AST(unexported(v.Field(i)), opt)
		if value == nil {
			continue // TODO: raise error? e.g. pointer to interface
		}
		keyValueExprs = append(keyValueExprs, &ast.KeyValueExpr{
			Key:   ast.NewIdent(v.Type().Field(i).Name),
			Value: value,
		})
	}
	return keyValueExprs
}
