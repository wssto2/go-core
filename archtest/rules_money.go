package archtest

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"
	"unicode"
)

func defaultMoneyNames() []string {
	return []string{
		"Price", "Amount", "Cost", "Total", "Discount", "Margin", "Fee", "Vat", "Tax", "Deposit",
		"Value", "Cents", "Net", "Gross",
	}
}

const defaultFloatMoneyWhy = "use money.Money (or money.Rate for a percent) and convert at the mapper: " +
	"money.FromFloat when reading the column, Money.Float when writing it back. " +
	"Only with a decision recorded in your architecture document: add it to NoFloatMoney.Exempt with a reason"

// words splits an identifier into its lower-case words: NetPrice -> net, price; VATRate -> vat, rate;
// NetworkID -> network, id; unit_price -> unit, price.
func words(ident string) []string {
	runes := []rune(ident)

	var out []string

	start := 0

	flush := func(end int) {
		if end > start {
			out = append(out, strings.ToLower(string(runes[start:end])))
		}

		start = end
	}

	for idx, char := range runes {
		switch {
		case char == '_' || unicode.IsDigit(char):
			flush(idx)

			start = idx + 1
		case unicode.IsUpper(char) && idx > 0 && (!unicode.IsUpper(runes[idx-1]) ||
			idx+1 < len(runes) && unicode.IsLower(runes[idx+1])):
			flush(idx)
		}
	}

	flush(len(runes))

	return out
}

// moneyLike reports whether one word of the identifier is a money name (or its plural).
func moneyLike(ident string, names []string) bool {
	for _, word := range words(ident) {
		if slices.ContainsFunc(names, func(name string) bool {
			name = strings.ToLower(name)

			return word == name || word == name+"s"
		}) {
			return true
		}
	}

	return false
}

// isFloat64 is float64, *float64, []float64 or []*float64.
func isFloat64(expr ast.Expr) bool {
	switch typ := expr.(type) {
	case *ast.Ident:
		return typ.Name == "float64"
	case *ast.StarExpr:
		return isFloat64(typ.X)
	case *ast.ArrayType:
		return isFloat64(typ.Elt)
	default:
		return false
	}
}

func (p *project) validateFloatMoney() error {
	for _, rule := range p.cfg.NoFloatMoney {
		for _, ex := range rule.Exempt {
			if ex.Field == "" || strings.TrimSpace(ex.Why) == "" {
				return fmt.Errorf("%w: NoFloatMoney.Exempt %q needs a Field and a Why", errConfig, ex.Field)
			}
		}
	}

	return nil
}

func (p *project) noFloatMoney() []string {
	var out []string

	for _, rule := range p.cfg.NoFloatMoney {
		names := rule.Names
		if len(names) == 0 {
			names = defaultMoneyNames()
		}

		matched := map[string]bool{}

		var found []finding

		for _, src := range p.files {
			if !placed(src, rule.Dirs, nil, false) {
				continue
			}

			for _, hit := range floatMoneyIn(src, names) {
				if slices.ContainsFunc(rule.Exempt, func(ex FloatMoneyExemption) bool { return ex.Field == hit.key }) {
					matched[hit.key] = true

					continue
				}

				found = append(found, hit)
			}
		}

		if len(found) > 0 {
			out = append(out, report("NO FLOAT MONEY", found, orDefault(rule.Why, defaultFloatMoneyWhy)))
		}

		out = append(out, staleFloatExemptions(rule.Exempt, matched)...)
	}

	return out
}

func staleFloatExemptions(exempt []FloatMoneyExemption, matched map[string]bool) []string {
	var stale []string

	for _, ex := range exempt {
		if !matched[ex.Field] {
			stale = append(stale, ex.Field)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	return []string{"STALE FLOAT MONEY EXEMPTION:\n  " + strings.Join(stale, "\n  ") +
		"\n\n  -> these names are no longer a float64 money name (or are gone): " +
		"remove them from NoFloatMoney.Exempt."}
}

// floatMoneyIn finds the float64 money names in the struct fields, interface methods and functions of
// one file. The finding's key is "pkg.Type.Field", "pkg.Func.param" or "pkg.Type.Method.param".
func floatMoneyIn(src Source, names []string) []finding {
	var out []finding

	check := func(prefix string, fields *ast.FieldList, kind string) {
		out = append(out, floatFields(src, names, prefix, fields, kind)...)
	}

	pkg := src.File.Name.Name

	for _, decl := range src.File.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			checkSignature(funcKey(pkg, decl), decl.Type, check)
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				if typ, isType := spec.(*ast.TypeSpec); isType {
					checkType(pkg, typ, check)
				}
			}
		}
	}

	return out
}

func funcKey(pkg string, decl *ast.FuncDecl) string {
	if decl.Recv != nil && len(decl.Recv.List) == 1 {
		return pkg + "." + recvName(decl.Recv.List[0].Type) + "." + decl.Name.Name
	}

	return pkg + "." + decl.Name.Name
}

func floatFields(src Source, names []string, prefix string, fields *ast.FieldList, kind string) []finding {
	if fields == nil {
		return nil
	}

	var out []finding

	for _, field := range fields.List {
		if !isFloat64(field.Type) {
			continue
		}

		for _, ident := range field.Names {
			if moneyLike(ident.Name, names) {
				key := prefix + "." + ident.Name
				out = append(out, finding{key: key, text: fmt.Sprintf(
					"%s  %s is a float64 %s named like money", src.At(ident), key, kind)})
			}
		}
	}

	return out
}

func checkSignature(prefix string, sig *ast.FuncType, check func(string, *ast.FieldList, string)) {
	check(prefix, sig.Params, "parameter")
	check(prefix, sig.Results, "result")
}

func checkType(pkg string, typ *ast.TypeSpec, check func(string, *ast.FieldList, string)) {
	prefix := pkg + "." + typ.Name.Name

	switch body := typ.Type.(type) {
	case *ast.StructType:
		check(prefix, body.Fields, "field")
	case *ast.InterfaceType:
		for _, method := range body.Methods.List {
			sig, isFunc := method.Type.(*ast.FuncType)
			if isFunc && len(method.Names) > 0 {
				checkSignature(prefix+"."+method.Names[0].Name, sig, check)
			}
		}
	}
}

// recvName is the type name of a receiver: T for T, *T and T[P].
func recvName(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.StarExpr:
		return recvName(typ.X)
	case *ast.IndexExpr:
		return recvName(typ.X)
	case *ast.IndexListExpr:
		return recvName(typ.X)
	case *ast.Ident:
		return typ.Name
	default:
		return ""
	}
}
