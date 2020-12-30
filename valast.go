package valast

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"

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

// String converts the value v into the equivalent Go literal syntax.
//
// It is an opinionated helper for the more extensive AST function.
//
// If any error occurs, it will be returned as the string value. If handling errors is desired then
// consider using the AST function directly.
func String(v interface{}) string {
	return StringWithOptions(v, nil)
}

// StringWithOptions converts the value v into the equivalent Go literal syntax, with the specified
// options.
//
// It is an opinionated helper for the more extensive AST function.
//
// If any error occurs, it will be returned as the string value. If handling errors is desired then
// consider using the AST function directly.
func StringWithOptions(v interface{}, opt *Options) string {
	if opt == nil {
		opt = &Options{}
	}
	var buf bytes.Buffer
	result, err := AST(reflect.ValueOf(v), opt)
	if err != nil {
		return err.Error()
	}
	if opt.ExportedOnly && result.RequiresUnexported {
		return fmt.Sprintf("valast: cannot convert unexported value %T", v)
	}
	if err := format.Node(&buf, token.NewFileSet(), result.AST); err != nil {
		return fmt.Sprintf("valast: format: %v", err)
	}
	return buf.String()
}

func basicLit(vv reflect.Value, kind token.Token, builtinType string, v interface{}, opt *Options) (Result, error) {
	typeExpr, err := typeExpr(vv.Type(), opt)
	if err != nil {
		return Result{}, err
	}
	if opt.Unqualify && vv.Type().Name() == builtinType && vv.Type().PkgPath() == "" {
		return Result{AST: ast.NewIdent(fmt.Sprint(v))}, nil
	}
	if opt.ExportedOnly && typeExpr.RequiresUnexported {
		return Result{RequiresUnexported: true}, nil
	}
	return Result{
		AST: &ast.CallExpr{
			Fun:  typeExpr.AST,
			Args: []ast.Expr{ast.NewIdent(fmt.Sprint(v))},
		},
		RequiresUnexported: typeExpr.RequiresUnexported,
	}, nil
}

// ErrInvalidType describes that the value is of a type that cannot be converted to an AST.
type ErrInvalidType struct {
	// Value is the actual value that was being converted.
	Value interface{}
}

// Error implements the error interface.
func (e *ErrInvalidType) Error() string {
	return fmt.Sprintf("valast: cannot convert value of type %T", e.Value)
}

// ErrPointerToInterface describes that a pointer to an interface was encountered, such values are
// impossible to create in a single Go expression and thus not supported by valast.
type ErrPointerToInterface struct {
	// Value is the actual pointer to the interface that was found.
	Value interface{}
}

// Error implements the error interface.
func (e *ErrPointerToInterface) Error() string {
	return fmt.Sprintf("valast: pointers to interfaces are not allowed, found %T", e.Value)
}

// Result is a result from converting a Go value into its AST.
type Result struct {
	// AST is the actual Go AST expression for the value.
	//
	// If Options.ExportedOnly == true, and the input value was unexported this field will be nil.
	AST ast.Expr

	// OmittedUnexported indicates if unexported fields were omitted or not. Only indicative if
	// Options.ExportedOnly == true.
	OmittedUnexported bool

	// RequiresUnexported indicates if the AST requires access to unexported types/values outside
	// of the package specified in the Options, and is thus invalid code.
	RequiresUnexported bool
}

// AST converts the given value into its equivalent Go AST expression.
//
// The input must be one of these kinds:
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
// The input type is reflect.Value instead of interface{}, specifically to allow converting
// interfaces derived from struct fields or other reflection which would otherwise be lost if the
// input type is interface{}.
func AST(v reflect.Value, opt *Options) (Result, error) {
	if opt == nil {
		opt = &Options{}
	}
	if v == (reflect.Value{}) {
		// Technically this is an invalid reflect.Value, but we handle it to be gracious in the
		// case of:
		//
		//  var x interface{}
		// 	valast.AST(reflect.ValueOf(x))
		//
		return Result{
			AST: ast.NewIdent("nil"),
		}, nil
	}

	vv := unexported(v)
	switch vv.Kind() {
	case reflect.Bool:
		boolType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		if vv.Type().Name() == "bool" && vv.Type().PkgPath() == "" {
			return Result{AST: ast.NewIdent(fmt.Sprint(v))}, nil
		}
		if opt.ExportedOnly && boolType.RequiresUnexported {
			return Result{RequiresUnexported: true}, nil
		}
		return Result{
			AST: &ast.CallExpr{
				Fun:  boolType.AST,
				Args: []ast.Expr{ast.NewIdent(fmt.Sprint(v))},
			},
			RequiresUnexported: boolType.RequiresUnexported,
		}, nil
	case reflect.Int:
		return basicLit(vv, token.INT, "int", v, opt)
	case reflect.Int8:
		return basicLit(vv, token.INT, "int8", v, opt)
	case reflect.Int16:
		return basicLit(vv, token.INT, "int16", v, opt)
	case reflect.Int32:
		return basicLit(vv, token.INT, "int32", v, opt)
	case reflect.Int64:
		return basicLit(vv, token.INT, "int64", v, opt)
	case reflect.Uint:
		return basicLit(vv, token.INT, "uint", v, opt)
	case reflect.Uint8:
		return basicLit(vv, token.INT, "uint8", v, opt)
	case reflect.Uint16:
		return basicLit(vv, token.INT, "uint16", v, opt)
	case reflect.Uint32:
		return basicLit(vv, token.INT, "uint32", v, opt)
	case reflect.Uint64:
		return basicLit(vv, token.INT, "uint64", v, opt)
	case reflect.Uintptr:
		return basicLit(vv, token.INT, "uintptr", v, opt)
	case reflect.Float32:
		return basicLit(vv, token.FLOAT, "float32", v, opt)
	case reflect.Float64:
		return basicLit(vv, token.FLOAT, "float64", v, opt)
	case reflect.Complex64:
		return basicLit(vv, token.FLOAT, "complex64", v, opt)
	case reflect.Complex128:
		return basicLit(vv, token.FLOAT, "complex128", v, opt)
	case reflect.Array:
		var (
			elts               []ast.Expr
			requiresUnexported bool
		)
		for i := 0; i < vv.Len(); i++ {
			elem, err := AST(vv.Index(i), opt.withUnqualify())
			if err != nil {
				return Result{}, err
			}
			if elem.RequiresUnexported {
				requiresUnexported = true
			}
			elts = append(elts, elem.AST)
		}
		arrayType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.CompositeLit{
				Type: arrayType.AST,
				Elts: elts,
			},
			RequiresUnexported: arrayType.RequiresUnexported || requiresUnexported,
		}, nil
	case reflect.Interface:
		if opt.ExportedOnly && !ast.IsExported(vv.Type().Name()) {
			return Result{
				AST:                nil,
				RequiresUnexported: true,
			}, nil
		}
		if opt.Unqualify {
			return AST(unexported(vv.Elem()), opt.withUnqualify())
		}
		v, err := AST(unexported(vv.Elem()), opt)
		if err != nil {
			return Result{}, err
		}
		interfaceType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.CompositeLit{
				Type: interfaceType.AST,
				Elts: []ast.Expr{v.AST},
			},
			RequiresUnexported: interfaceType.RequiresUnexported || v.RequiresUnexported,
		}, nil
	case reflect.Map:
		var (
			keyValueExprs                         []ast.Expr
			requiresUnexported, omittedUnexported bool
			keys                                  = vv.MapKeys()
		)
		sort.Slice(keys, func(i, j int) bool {
			return valueLess(keys[i], keys[j])
		})
		for _, key := range keys {
			value := vv.MapIndex(key)
			k, err := AST(key, opt.withUnqualify())
			if err != nil {
				return Result{}, err
			}
			if k.RequiresUnexported {
				if opt.ExportedOnly {
					omittedUnexported = true
					continue
				}
				requiresUnexported = true
			}
			if k.OmittedUnexported {
				omittedUnexported = true
			}
			v, err := AST(value, opt.withUnqualify())
			if err != nil {
				return Result{}, err
			}
			if v.RequiresUnexported {
				if opt.ExportedOnly {
					omittedUnexported = true
					continue
				}
				requiresUnexported = true
			}
			if v.OmittedUnexported {
				omittedUnexported = true
			}
			keyValueExprs = append(keyValueExprs, &ast.KeyValueExpr{
				Key:   k.AST,
				Value: v.AST,
			})
		}
		mapType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.CompositeLit{
				Type: mapType.AST,
				Elts: keyValueExprs,
			},
			RequiresUnexported: requiresUnexported || mapType.RequiresUnexported,
			OmittedUnexported:  omittedUnexported,
		}, nil
	case reflect.Ptr:
		if vv.Elem().Kind() == reflect.Interface {
			// Pointer to interface; cannot be created in a single expression.
			return Result{}, &ErrPointerToInterface{Value: vv.Interface()}
		}
		ptrType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		if vv.IsNil() {
			if opt.Unqualify {
				return Result{AST: ast.NewIdent("nil")}, nil
			}
			return Result{
				AST: &ast.CallExpr{
					Fun:  &ast.ParenExpr{X: ptrType.AST},
					Args: []ast.Expr{ast.NewIdent("nil")},
				},
				RequiresUnexported: ptrType.RequiresUnexported,
			}, nil
		}
		if opt.ExportedOnly && ptrType.RequiresUnexported {
			return Result{RequiresUnexported: true}, nil
		}
		elem, err := AST(vv.Elem(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.UnaryExpr{
				Op: token.AND,
				X:  elem.AST,
			},
			RequiresUnexported: ptrType.RequiresUnexported || elem.RequiresUnexported,
			OmittedUnexported:  elem.OmittedUnexported,
		}, nil
	case reflect.Slice:
		var (
			elts               []ast.Expr
			requiresUnexported bool
		)
		for i := 0; i < vv.Len(); i++ {
			elem, err := AST(vv.Index(i), opt.withUnqualify())
			if err != nil {
				return Result{}, err
			}
			if elem.RequiresUnexported {
				requiresUnexported = true
			}
			elts = append(elts, elem.AST)
		}
		sliceType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.CompositeLit{
				Type: sliceType.AST,
				Elts: elts,
			},
			RequiresUnexported: requiresUnexported || sliceType.RequiresUnexported,
		}, nil
	case reflect.String:
		s := v.String()
		if len(s) > 40 && strings.Contains(s, "\n") && !strings.Contains(s, "`") {
			return basicLit(vv, token.STRING, "string", "`"+s+"`", opt.withUnqualify())
		}
		return basicLit(vv, token.STRING, "string", strconv.Quote(v.String()), opt.withUnqualify())
	case reflect.Struct:
		var (
			structValue                           []ast.Expr
			requiresUnexported, omittedUnexported bool
		)
		for i := 0; i < v.NumField(); i++ {
			if unexported(v.Field(i)).IsZero() {
				continue
			}
			value, err := AST(unexported(v.Field(i)), opt.withUnqualify())
			if err != nil {
				return Result{}, err
			}
			if value.RequiresUnexported {
				if opt.ExportedOnly {
					omittedUnexported = true
					continue
				}
				requiresUnexported = true
			}
			if value.OmittedUnexported {
				omittedUnexported = true
			}
			structValue = append(structValue, &ast.KeyValueExpr{
				Key:   ast.NewIdent(v.Type().Field(i).Name),
				Value: value.AST,
			})
		}
		structType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		if opt.ExportedOnly && structType.RequiresUnexported {
			return Result{RequiresUnexported: true}, nil
		}
		return Result{
			AST: &ast.CompositeLit{
				Type: structType.AST,
				Elts: structValue,
			},
			RequiresUnexported: structType.RequiresUnexported || requiresUnexported,
			OmittedUnexported:  omittedUnexported,
		}, nil
	case reflect.UnsafePointer:
		unsafePointerType, err := typeExpr(vv.Type(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.CallExpr{
				Fun: unsafePointerType.AST,
				Args: []ast.Expr{
					&ast.CallExpr{
						Fun:  ast.NewIdent("uintptr"),
						Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("0x%x", v.Pointer())}},
					},
				},
			},
			RequiresUnexported: unsafePointerType.RequiresUnexported,
			OmittedUnexported:  unsafePointerType.OmittedUnexported,
		}, nil
	default:
		return Result{AST: nil}, &ErrInvalidType{Value: v.Interface()}
	}
}

// typeExpr returns an AST type expression for the value v.
func typeExpr(v reflect.Type, opt *Options) (Result, error) {
	switch v.Kind() {
	case reflect.Array:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{AST: ast.NewIdent(v.Name())}, nil
		}
		elemType, err := typeExpr(v.Elem(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.ArrayType{
				Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(v.Len())},
				Elt: elemType.AST,
			},
			RequiresUnexported: elemType.RequiresUnexported,
		}, nil
	case reflect.Interface:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{AST: ast.NewIdent(v.Name())}, nil
		}
		var methods []*ast.Field
		var requiresUnexported bool
		for i := 0; i < v.NumMethod(); i++ {
			method := v.Method(i)
			methodType, err := typeExpr(method.Type, opt)
			if err != nil {
				return Result{}, err
			}
			if methodType.RequiresUnexported {
				requiresUnexported = true
			}
			methods = append(methods, &ast.Field{
				Names: []*ast.Ident{ast.NewIdent(method.Name)},
				Type:  methodType.AST,
			})
		}
		return Result{
			AST:                &ast.InterfaceType{Methods: &ast.FieldList{List: methods}},
			RequiresUnexported: requiresUnexported,
		}, nil
	case reflect.Func:
		// Note: reflect cannot determine parameter/result names. See https://groups.google.com/g/golang-nuts/c/nM_ZhL7fuGc
		var (
			requiresUnexported bool
			params             []*ast.Field
		)
		for i := 0; i < v.NumIn(); i++ {
			param := v.In(i)
			paramType, err := typeExpr(param, opt)
			if err != nil {
				return Result{}, err
			}
			if paramType.RequiresUnexported {
				requiresUnexported = true
			}
			params = append(params, &ast.Field{
				Type: paramType.AST,
			})
		}
		var results []*ast.Field
		for i := 0; i < v.NumOut(); i++ {
			result := v.Out(i)
			resultType, err := typeExpr(result, opt)
			if err != nil {
				return Result{}, err
			}
			if resultType.RequiresUnexported {
				requiresUnexported = true
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
			RequiresUnexported: requiresUnexported,
		}, nil
	case reflect.Map:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{
				AST:                ast.NewIdent(v.Name()),
				RequiresUnexported: false,
			}, nil
		}
		keyType, err := typeExpr(v.Key(), opt)
		if err != nil {
			return Result{}, err
		}
		valueType, err := typeExpr(v.Elem(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST: &ast.MapType{
				Key:   keyType.AST,
				Value: valueType.AST,
			},
			RequiresUnexported: keyType.RequiresUnexported || valueType.RequiresUnexported,
		}, nil
	case reflect.Ptr:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{AST: ast.NewIdent(v.Name())}, nil
		}
		ptrType, err := typeExpr(v.Elem(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST:                &ast.StarExpr{X: ptrType.AST},
			RequiresUnexported: ptrType.RequiresUnexported,
		}, nil
	case reflect.Slice:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{AST: ast.NewIdent(v.Name())}, nil
		}
		elemType, err := typeExpr(v.Elem(), opt)
		if err != nil {
			return Result{}, err
		}
		return Result{
			AST:                &ast.ArrayType{Elt: elemType.AST},
			RequiresUnexported: elemType.RequiresUnexported,
		}, nil
	case reflect.Struct:
		if v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{
				AST:                ast.NewIdent(v.Name()),
				RequiresUnexported: false,
			}, nil
		}
		var (
			fields                                []*ast.Field
			requiresUnexported, omittedUnexported bool
		)
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			fieldType, err := typeExpr(field.Type, opt)
			if err != nil {
				return Result{}, err
			}
			if fieldType.RequiresUnexported {
				requiresUnexported = true
				if opt.ExportedOnly {
					return Result{RequiresUnexported: true}, nil
				}
			}
			if fieldType.OmittedUnexported {
				omittedUnexported = true
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
			RequiresUnexported: requiresUnexported,
			OmittedUnexported:  omittedUnexported,
		}, nil
	case reflect.UnsafePointer:
		// Note: For a plain unsafe.Pointer type, v.PkgPath() does not report "unsafe" but rather
		// an empty string "".
		isPlainUnsafePointer := v.String() == "unsafe.Pointer"
		if !isPlainUnsafePointer && v.Name() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != "" && pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(v.PkgPath())
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST:                &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
			return Result{
				AST:                ast.NewIdent(v.Name()),
				RequiresUnexported: false,
			}, nil
		}
		return Result{AST: &ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent("Pointer")}}, nil
	default:
		if v.PkgPath() != "" {
			pkgPath := v.PkgPath()
			if pkgPath != opt.PackagePath {
				pkgName, err := opt.packagePathToName(pkgPath)
				if err != nil {
					return Result{}, err
				}
				if pkgName != opt.PackageName {
					return Result{
						AST: &ast.CallExpr{
							Fun:  &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: ast.NewIdent(v.Name())},
							Args: []ast.Expr{ast.NewIdent(fmt.Sprint(v))},
						},
						RequiresUnexported: !ast.IsExported(v.Name()),
					}, nil
				}
			}
		}
		return Result{AST: ast.NewIdent(v.Name())}, nil
	}
}

func unexported(v reflect.Value) reflect.Value {
	if v == (reflect.Value{}) {
		return v
	}
	return bypass.UnsafeReflectValue(v)
}

// valueLess tells if i is less than j, according to normal Go less-than < operator rules. Values
// that are unsortable according to Go rules will always yield true.
//
// The two values must be of the same kind or a panic will occur.
func valueLess(i, j reflect.Value) bool {
	ii := unexported(i)
	switch ii.Kind() {
	case reflect.Bool:
		x := 0
		if ii.Bool() {
			x = 1
		}
		y := 0
		if unexported(j).Bool() {
			y = 1
		}
		return x < y
	case reflect.Int:
		return ii.Int() < unexported(j).Int()
	case reflect.Int8:
		return ii.Int() < unexported(j).Int()
	case reflect.Int16:
		return ii.Int() < unexported(j).Int()
	case reflect.Int32:
		return ii.Int() < unexported(j).Int()
	case reflect.Int64:
		return ii.Int() < unexported(j).Int()
	case reflect.Uint:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Uint8:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Uint16:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Uint32:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Uint64:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Uintptr:
		return ii.Uint() < unexported(j).Uint()
	case reflect.Float32:
		return ii.Float() < unexported(j).Float()
	case reflect.Float64:
		return ii.Float() < unexported(j).Float()
	case reflect.Ptr:
		return ii.Pointer() < unexported(j).Pointer()
	case reflect.String:
		return ii.String() < unexported(j).String()
	case reflect.UnsafePointer:
		return ii.Pointer() < unexported(j).Pointer()
	case reflect.Complex64:
		return true
	case reflect.Complex128:
		return true
	case reflect.Array:
		return true
	case reflect.Map:
		return true
	case reflect.Interface:
		return true
	case reflect.Slice:
		return true
	case reflect.Struct:
		return true
	default:
		// never here
		return true
	}
}
