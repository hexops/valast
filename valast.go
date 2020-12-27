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
	result := AST(v, opt)
	if result.AST == nil {
		var typ = "nil"
		if v != (reflect.Value{}) {
			typ = fmt.Sprintf("%T", v.Interface())
		}
		return "", fmt.Errorf("valast: cannot convert value of kind:%s type:%s", v.Kind(), typ)
	}
	if err := format.Node(&buf, token.NewFileSet(), result.AST); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func basicLit(kind token.Token, typ string, v interface{}, opt *Options) Result {
	if opt.Unqualify {
		return Result{
			AST:                &ast.BasicLit{Kind: kind, Value: fmt.Sprint(v)},
			ContainsUnexported: false,
		}
	}
	return Result{
		AST: &ast.CallExpr{
			Fun:  ast.NewIdent(typ),
			Args: []ast.Expr{&ast.BasicLit{Kind: kind, Value: fmt.Sprint(v)}},
		},
		ContainsUnexported: false,
	}
}

// Result is a result from converting a Go value into its AST.
type Result struct {
	// AST is the actual Go AST expression for the value, or nil if the value could not be
	// converted.
	AST ast.Expr

	// ContainsUnexported indicates if the AST references unexported types/values, excluding those
	// defined in the package specified in the Options. i.e. whether or not the code will be valid
	// according to exportation rules.
	//
	// If Options.ExportedOnly == true, this field signifies if unexported fields were omitted.
	ContainsUnexported bool
}

// AST is identical to String, except it returns an AST and other metadata about the AST.
func AST(v reflect.Value, opt *Options) Result {
	if v == (reflect.Value{}) {
		// Technically this is an invalid reflect.Value, but we handle it to be gracious in the
		// case of:
		//
		//  var x interface{}
		// 	valast.AST(reflect.ValueOf(x))
		//
		return Result{
			AST:                ast.NewIdent("nil"),
			ContainsUnexported: false,
		}
	}

	vv := unexported(v)
	switch vv.Kind() {
	case reflect.Bool:
		if opt.Unqualify {
			return Result{
				AST:                ast.NewIdent(fmt.Sprint(v)),
				ContainsUnexported: false,
			}
		}
		return Result{
			AST: &ast.CallExpr{
				Fun:  ast.NewIdent("bool"),
				Args: []ast.Expr{ast.NewIdent(fmt.Sprint(v))},
			},
			ContainsUnexported: false,
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
		var (
			elts               []ast.Expr
			containsUnexported bool
		)
		for i := 0; i < vv.Len(); i++ {
			elem := AST(vv.Index(i), opt)
			if elem.ContainsUnexported {
				containsUnexported = true
			}
			elts = append(elts, elem.AST)
		}
		arrayType := typeExpr(vv.Type(), opt)
		return Result{
			AST: &ast.CompositeLit{
				Type: arrayType.AST,
				Elts: elts,
			},
			ContainsUnexported: arrayType.ContainsUnexported || containsUnexported,
		}
	case reflect.Interface:
		if opt.ExportedOnly && !ast.IsExported(vv.Type().Name()) {
			return Result{
				AST:                nil,
				ContainsUnexported: true,
			}
		}
		if opt.Unqualify {
			return AST(unexported(vv.Elem()), opt.withUnqualify())
		}
		v := AST(unexported(vv.Elem()), opt)
		interfaceType := typeExpr(vv.Type(), opt)
		return Result{
			AST: &ast.CompositeLit{
				Type: interfaceType.AST,
				Elts: []ast.Expr{v.AST},
			},
			ContainsUnexported: interfaceType.ContainsUnexported || v.ContainsUnexported,
		}
	case reflect.Map:
		// TODO: stable sorting of map keys
		var (
			keyValueExprs      []ast.Expr
			containsUnexported bool
			keys               = vv.MapKeys()
		)
		for _, key := range keys {
			value := vv.MapIndex(key)
			k := AST(key, opt.withUnqualify())
			if k.ContainsUnexported {
				containsUnexported = true
			}
			v := AST(value, opt.withUnqualify())
			if v.ContainsUnexported {
				containsUnexported = true
			}
			keyValueExprs = append(keyValueExprs, &ast.KeyValueExpr{
				Key:   k.AST,
				Value: v.AST,
			})
		}
		mapType := typeExpr(vv.Type(), opt.withUnqualify())
		return Result{
			AST: &ast.CompositeLit{
				Type: mapType.AST,
				Elts: keyValueExprs,
			},
			ContainsUnexported: containsUnexported || mapType.ContainsUnexported,
		}
	case reflect.Ptr:
		opt.Unqualify = false
		if vv.Elem().Kind() == reflect.Interface {
			// Pointer to interface; cannot be created in a single expression.
			//
			// TODO: turn this into an error
			return Result{AST: nil, ContainsUnexported: false}
		}
		elem := AST(vv.Elem(), opt)
		return Result{
			AST: &ast.UnaryExpr{
				Op: token.AND,
				X:  elem.AST,
			},
			ContainsUnexported: elem.ContainsUnexported,
		}
	case reflect.Slice:
		var (
			elts               []ast.Expr
			containsUnexported bool
		)
		for i := 0; i < vv.Len(); i++ {
			elem := AST(vv.Index(i), opt)
			if elem.ContainsUnexported {
				containsUnexported = true
			}
			elts = append(elts, elem.AST)
		}
		sliceType := typeExpr(vv.Type(), opt)
		return Result{
			AST: &ast.CompositeLit{
				Type: sliceType.AST,
				Elts: elts,
			},
			ContainsUnexported: containsUnexported || sliceType.ContainsUnexported,
		}
	case reflect.String:
		// TODO: format long strings, strings with unicode, etc. more nicely
		return basicLit(token.STRING, "string", strconv.Quote(v.String()), opt)
	case reflect.Struct:
		if opt.ExportedOnly && !ast.IsExported(vv.Type().Name()) {
			return Result{AST: nil}
		}
		var (
			structValue        []ast.Expr
			containsUnexported bool
		)
		for i := 0; i < v.NumField(); i++ {
			if opt.ExportedOnly && !ast.IsExported(v.Type().Field(i).Name) {
				continue
			}
			if unexported(v.Field(i)).IsZero() {
				continue
			}
			value := AST(unexported(v.Field(i)), opt.withUnqualify())
			if value.ContainsUnexported {
				containsUnexported = true
			}
			if value.AST == nil {
				continue // TODO: raise error? e.g. pointer to interface
			}
			structValue = append(structValue, &ast.KeyValueExpr{
				Key:   ast.NewIdent(v.Type().Field(i).Name),
				Value: value.AST,
			})
		}
		structType := typeExpr(vv.Type(), opt)
		return Result{
			AST: &ast.CompositeLit{
				Type: structType.AST,
				Elts: structValue,
			},
			ContainsUnexported: structType.ContainsUnexported || containsUnexported,
		}
	case reflect.UnsafePointer:
		return Result{
			AST: &ast.CallExpr{
				Fun: &ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent("Pointer")},
				Args: []ast.Expr{
					&ast.CallExpr{
						Fun:  ast.NewIdent("uintptr"),
						Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("0x%x", v.Pointer())}},
					},
				},
			},
			ContainsUnexported: false,
		}
	default:
		// TODO: make this an error
		return Result{AST: nil}
	}
}

// typeExpr returns an AST type expression for the value v.
func typeExpr(v reflect.Type, opt *Options) Result {
	switch v.Kind() {
	case reflect.Array:
		// TODO: omit if not exported and Options.ExportedOnly
		elem := typeExpr(v.Elem(), opt)
		return Result{
			AST: &ast.ArrayType{
				Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(v.Len())},
				Elt: elem.AST,
			},
			ContainsUnexported: elem.ContainsUnexported,
		}
	case reflect.Interface:
		// TODO: omit if not exported and Options.ExportedOnly
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				// TODO: bubble up errors
				pkgName, _ := opt.packagePathToName(v.PkgPath())
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						ContainsUnexported: !ast.IsExported(v.Name()),
					}
				}
			}
			return Result{
				AST:                ast.NewIdent(v.Name()),
				ContainsUnexported: false,
			}
		}
		var methods []*ast.Field
		var containsUnexported bool
		for i := 0; i < v.NumMethod(); i++ {
			method := v.Method(i)
			methodType := typeExpr(method.Type, opt)
			// TODO: omit if not exported and Options.ExportedOnly
			if methodType.ContainsUnexported {
				containsUnexported = true
			}
			methods = append(methods, &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(method.Name)},
				Type:  methodType.AST,
			})
		}
		return Result{
			AST:                &ast.InterfaceType{Methods: &ast.FieldList{List: methods}},
			ContainsUnexported: containsUnexported,
		}
	case reflect.Func:
		// TODO: omit if not exported and Options.ExportedOnly
		// Note: reflect cannot determine parameter/result names. See https://groups.google.com/g/golang-nuts/c/nM_ZhL7fuGc
		var (
			containsUnexported bool
			params             []*ast.Field
		)
		for i := 0; i < v.NumIn(); i++ {
			// TODO: omit if not exported and Options.ExportedOnly
			param := v.In(i)
			paramType := typeExpr(param, opt)
			if paramType.ContainsUnexported {
				containsUnexported = true
			}
			params = append(params, &ast.Field{
				Type: paramType.AST,
			})
		}
		var results []*ast.Field
		for i := 0; i < v.NumOut(); i++ {
			// TODO: omit if not exported and Options.ExportedOnly
			result := v.Out(i)
			resultType := typeExpr(result, opt)
			if resultType.ContainsUnexported {
				containsUnexported = true
			}
			results = append(results, &ast.Field{
				Type: resultType.AST,
			})
		}
		return Result{
			AST: &ast.FuncType{
				Params:  &ast.FieldList{List: params},
				Results: &ast.FieldList{List: results},
			},
			ContainsUnexported: containsUnexported,
		}
	case reflect.Map:
		// TODO: omit if not exported and Options.ExportedOnly
		keyType := typeExpr(v.Key(), opt)
		valueType := typeExpr(v.Elem(), opt)
		return Result{
			AST: &ast.MapType{
				Key:   keyType.AST,
				Value: valueType.AST,
			},
			ContainsUnexported: keyType.ContainsUnexported || valueType.ContainsUnexported,
		}
	case reflect.Ptr:
		// TODO: omit if not exported and Options.ExportedOnly
		ptrType := typeExpr(v.Elem(), opt)
		return Result{
			AST:                &ast.StarExpr{X: ptrType.AST},
			ContainsUnexported: ptrType.ContainsUnexported,
		}
	case reflect.Slice:
		// TODO: omit if not exported and Options.ExportedOnly
		sliceType := typeExpr(v.Elem(), opt)
		return Result{
			AST:                &ast.ArrayType{Elt: sliceType.AST},
			ContainsUnexported: sliceType.ContainsUnexported,
		}
	case reflect.Struct:
		// TODO: omit if not exported and Options.ExportedOnly
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				// TODO: bubble up errors
				pkgName, _ := opt.packagePathToName(v.PkgPath())
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						ContainsUnexported: !ast.IsExported(v.Name()),
					}
				}
			}
			return Result{
				AST:                ast.NewIdent(v.Name()),
				ContainsUnexported: false,
			}
		}
		var (
			fields             []*ast.Field
			containsUnexported bool
		)
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldType := typeExpr(field.Type, opt)
			if fieldType.ContainsUnexported {
				containsUnexported = true
			}
			fields = append(fields, &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(field.Name)},
				Type:  fieldType.AST,
			})
		}
		return Result{
			AST: &ast.StructType{
				Fields: &ast.FieldList{List: fields},
			},
			ContainsUnexported: containsUnexported,
		}
	default:
		return Result{
			AST:                ast.NewIdent(v.Name()),
			ContainsUnexported: false,
		}
	}
}

func unexported(v reflect.Value) reflect.Value {
	if v == (reflect.Value{}) {
		return v
	}
	return bypass.UnsafeReflectValue(v)
}
