package archtest

import (
	"fmt"
	"go/ast"
	"path"
	"slices"
	"strings"
)

const auditPackage = "github.com/wssto2/go-core/audit"

func defaultWritePrefixes() []string {
	return []string{"Create", "Update", "Save", "Delete", "Archive", "Insert", "Upsert", "Set", "Add", "Remove", "Mark"}
}

func defaultTxCalls() []string { return []string{"WithinTx", "WithinTransaction"} }

// serviceType is a struct of a package whose methods the rules inspect.
type serviceType struct {
	repos  map[string]bool // fields typed as a repository interface
	audits map[string]bool // fields typed from go-core's audit package
}

// serviceFunc is a function or method of a package, found by "Type.Name" or ".Name".
type serviceFunc struct {
	src  Source
	decl *ast.FuncDecl
	typ  string
	recv string
}

// writeUse is one repository write reached from a method.
type writeUse struct {
	field, call, at string
	inTx            bool
}

// reach is what a method reaches.
type reach struct {
	writes  []writeUse
	audited bool
}

// serviceMethod is an exported method of a service type.
type serviceMethod struct {
	name  string // package.Type.Method
	at    string
	reach reach
}

// servicePackage indexes one directory.
type servicePackage struct {
	funcs map[string]serviceFunc
	types map[string]*serviceType
}

func (writes ServiceWrites) withDefaults() ServiceWrites {
	if len(writes.Types) == 0 {
		writes.Types = []string{"Service"}
	}

	if len(writes.RepositoryNames) == 0 {
		writes.RepositoryNames = []string{"Repository"}
	}

	if len(writes.Prefixes) == 0 {
		writes.Prefixes = defaultWritePrefixes()
	}

	return writes
}

// indexPackages groups the production files of Dirs by directory.
func (p *project) indexPackages(writes ServiceWrites) map[string]*servicePackage {
	pkgs := map[string]*servicePackage{}

	for _, src := range p.files {
		if !placed(src, writes.Dirs, nil, false) {
			continue
		}

		dir := path.Dir(src.Rel)

		pkg := pkgs[dir]
		if pkg == nil {
			pkg = &servicePackage{funcs: map[string]serviceFunc{}, types: map[string]*serviceType{}}
			pkgs[dir] = pkg
		}

		for _, decl := range src.File.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				typ, recv := receiverOf(typed)
				pkg.funcs[typ+"."+typed.Name.Name] = serviceFunc{src: src, decl: typed, typ: typ, recv: recv}
			case *ast.GenDecl:
				p.indexTypes(pkg, src, typed, writes)
			}
		}
	}

	return pkgs
}

func (p *project) indexTypes(pkg *servicePackage, src Source, decl *ast.GenDecl, writes ServiceWrites) {
	for _, spec := range decl.Specs {
		typeSpec, isType := spec.(*ast.TypeSpec)
		if !isType || !hasSuffixIn(typeSpec.Name.Name, writes.Types) {
			continue
		}

		structType, isStruct := typeSpec.Type.(*ast.StructType)
		if !isStruct {
			continue
		}

		one := &serviceType{repos: map[string]bool{}, audits: map[string]bool{}}

		for _, field := range structType.Fields.List {
			ftype := field.Type
			if star, isStar := ftype.(*ast.StarExpr); isStar {
				ftype = star.X
			}

			sel, isSel := ftype.(*ast.SelectorExpr)
			if !isSel {
				continue
			}

			imported := importOfAlias(src.File, sel.X)
			for _, name := range field.Names {
				one.repos[name.Name] = p.isRepositoryImport(imported, writes) && hasSuffixIn(sel.Sel.Name, writes.RepositoryNames)
				one.audits[name.Name] = imported == auditPackage
			}
		}

		pkg.types[typeSpec.Name.Name] = one
	}
}

// isRepositoryImport reports whether an import path is under one of writes.Repositories.
func (p *project) isRepositoryImport(imported string, writes ServiceWrites) bool {
	return slices.ContainsFunc(writes.Repositories, func(dir string) bool { return under(imported, p.module+"/"+dir) })
}

// importOfAlias is the import path the file gives the identifier ("" when it is no package name).
func importOfAlias(file *ast.File, expr ast.Expr) string {
	ident, isIdent := expr.(*ast.Ident)
	if !isIdent {
		return ""
	}

	for _, imp := range file.Imports {
		name := lastSegment(importPath(imp))
		if imp.Name != nil {
			name = imp.Name.Name
		}

		if name == ident.Name {
			return importPath(imp)
		}
	}

	return ""
}

func hasSuffixIn(name string, suffixes []string) bool {
	return slices.ContainsFunc(suffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
}

// receiverOf is the type name and receiver identifier of a method ("" for a function).
func receiverOf(decl *ast.FuncDecl) (string, string) {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return "", ""
	}

	field := decl.Recv.List[0]
	expr := field.Type

	if star, isStar := expr.(*ast.StarExpr); isStar {
		expr = star.X
	}

	if idx, isIdx := expr.(*ast.IndexExpr); isIdx {
		expr = idx.X
	}

	name, recv := "", ""
	if ident, isIdent := expr.(*ast.Ident); isIdent {
		name = ident.Name
	}

	if len(field.Names) > 0 {
		recv = field.Names[0].Name
	}

	return name, recv
}

// serviceMethods lists the exported methods of the service types, with what each reaches.
func (p *project) serviceMethods(writes ServiceWrites, audit []string, txCalls []string) []serviceMethod {
	var out []serviceMethod

	for _, pkg := range p.indexPackages(writes) {
		for _, target := range pkg.funcs {
			if target.typ == "" || !target.decl.Name.IsExported() || pkg.types[target.typ] == nil || target.decl.Body == nil {
				continue
			}

			scan := &scanner{
				p: p, pkg: pkg, writes: writes, audit: audit, txNames: txCalls,
				seen: map[string]bool{}, reach: reach{writes: nil, audited: false},
			}
			scan.walk(target, target.decl.Body, false)

			out = append(out, serviceMethod{
				name:  target.src.File.Name.Name + "." + target.typ + "." + target.decl.Name.Name,
				at:    target.src.At(target.decl),
				reach: scan.reach,
			})
		}
	}

	slices.SortFunc(out, func(a, b serviceMethod) int { return strings.Compare(a.name, b.name) })

	return out
}

// scanner walks a method and the same-package helpers it calls.
type scanner struct {
	p       *project
	pkg     *servicePackage
	writes  ServiceWrites
	audit   []string
	txNames []string
	seen    map[string]bool
	reach   reach
}

// walk records the writes and audit calls under node, following helper calls with the same transaction state.
func (s *scanner) walk(target serviceFunc, node ast.Node, inTx bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}

		s.classify(target, call, inTx)

		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !slices.Contains(s.txNames, sel.Sel.Name) {
			return true
		}

		s.walk(target, call.Fun, inTx)

		for _, arg := range call.Args {
			if lit, isLit := arg.(*ast.FuncLit); isLit {
				s.walk(target, lit.Body, true)
			} else {
				s.walk(target, arg, inTx)
			}
		}

		return false
	})
}

func (s *scanner) classify(target serviceFunc, call *ast.CallExpr, inTx bool) {
	typ := s.pkg.types[target.typ]

	if s.isAudit(target, typ, call) {
		s.reach.audited = true
	}

	switch fun := call.Fun.(type) {
	case *ast.Ident:
		s.follow(".", fun.Name, inTx)
	case *ast.SelectorExpr:
		s.classifySelector(target, typ, call, fun, inTx)
	}
}

func (s *scanner) classifySelector(
	target serviceFunc, typ *serviceType, call *ast.CallExpr, fun *ast.SelectorExpr, inTx bool,
) {
	switch recv := fun.X.(type) {
	case *ast.Ident:
		if recv.Name == target.recv && target.recv != "" {
			s.follow(target.typ+".", fun.Sel.Name, inTx)
		}
	case *ast.SelectorExpr:
		field := recv.Sel.Name
		if isIdent(recv.X, target.recv) && typ != nil && typ.repos[field] && hasPrefixIn(fun.Sel.Name, s.writes.Prefixes) {
			s.reach.writes = append(s.reach.writes, writeUse{
				field: field, call: field + "." + fun.Sel.Name, at: target.src.At(call), inTx: inTx,
			})
		}
	}
}

// follow descends into a same-package helper once per transaction state.
func (s *scanner) follow(prefix, name string, inTx bool) {
	key := fmt.Sprintf("%s%s|%t", prefix, name, inTx)
	if s.seen[key] {
		return
	}

	s.seen[key] = true

	if helper, found := s.pkg.funcs[prefix+name]; found && helper.decl.Body != nil {
		s.walk(helper, helper.decl.Body, inTx)
	}
}

func (s *scanner) isAudit(target serviceFunc, typ *serviceType, call *ast.CallExpr) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return false
	}

	if len(s.audit) == 0 {
		return defaultAudit(target, typ, sel)
	}

	return slices.ContainsFunc(s.audit, func(want string) bool {
		if method, isMethod := strings.CutPrefix(want, "."); isMethod {
			return sel.Sel.Name == method
		}

		pkg, member := splitMember(want)
		alias := aliasOf(target.src.File, pkg)

		return alias != "" && isIdent(sel.X, alias) && sel.Sel.Name == member
	})
}

// defaultAudit is Write or Record on a field of the service typed from go-core's audit package.
func defaultAudit(target serviceFunc, typ *serviceType, sel *ast.SelectorExpr) bool {
	field, isField := sel.X.(*ast.SelectorExpr)

	return isField && isIdent(field.X, target.recv) && typ != nil && typ.audits[field.Sel.Name] &&
		(sel.Sel.Name == "Write" || sel.Sel.Name == "Record")
}

func hasPrefixIn(name string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

const defaultAuditWhy = "call audit.Record in the method (in the same transaction as the write), or - only with a " +
	"decision recorded in your architecture document - add it to RequiresAudit.Exempt with a reason"

func (p *project) requiresAudit() []string {
	var out []string

	for _, req := range p.cfg.RequiresAudit {
		writes := req.Writes.withDefaults()
		matched := map[string]bool{}

		var found []finding

		for _, method := range p.serviceMethods(writes, req.Audit, nil) {
			if len(method.reach.writes) == 0 || method.reach.audited {
				continue
			}

			if slices.ContainsFunc(req.Exempt, func(ex AuditExemption) bool { return ex.Method == method.name }) {
				matched[method.name] = true

				continue
			}

			first := method.reach.writes[0]
			found = append(found, finding{key: method.name, text: fmt.Sprintf(
				"%s  %s writes (%s at %s) and reaches no audit write", method.at, method.name, first.call, first.at)})
		}

		if len(found) > 0 {
			out = append(out, report("REQUIRES AUDIT", found, orDefault(req.Why, defaultAuditWhy)))
		}

		var stale []string

		for _, ex := range req.Exempt {
			if !matched[ex.Method] {
				stale = append(stale, ex.Method)
			}
		}

		if len(stale) > 0 {
			out = append(out, "STALE AUDIT EXEMPTION:\n  "+strings.Join(stale, "\n  ")+
				"\n\n  -> these methods no longer write without an audit entry (or are gone): "+
				"remove them from RequiresAudit.Exempt.")
		}
	}

	return out
}

func (p *project) atomicWrites() []string {
	var out []string

	for _, req := range p.cfg.AtomicWrites {
		txNames := req.Tx
		if len(txNames) == 0 {
			txNames = defaultTxCalls()
		}

		var found []finding

		for _, method := range p.serviceMethods(req.Writes.withDefaults(), nil, txNames) {
			fields := map[string]bool{}
			outside := ""

			for _, write := range method.reach.writes {
				fields[write.field] = true

				if !write.inTx && outside == "" {
					outside = write.call + " at " + write.at
				}
			}

			if len(fields) >= 2 && outside != "" {
				found = append(found, finding{key: method.name, text: fmt.Sprintf(
					"%s  %s writes through %d repositories, %s is outside a transaction",
					method.at, method.name, len(fields), outside)})
			}
		}

		if len(found) > 0 {
			out = append(out, report("ATOMIC WRITES", found, orDefault(req.Why,
				"run the writes inside one "+strings.Join(txNames, "/")+
					" closure so a failure in the middle leaves nothing half-saved")))
		}
	}

	return out
}
