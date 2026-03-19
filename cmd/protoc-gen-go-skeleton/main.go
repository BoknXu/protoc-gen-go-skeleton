package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/compiler/protogen"
)

func main() {
	var flags flag.FlagSet
	var domain string
	// 可选参数：限定只处理某个业务域目录（例如 welcome）。
	flags.StringVar(&domain, "domain", "", "仅生成某个 domain 目录下的 proto")

	opts := protogen.Options{
		ParamFunc: flags.Set,
	}

	opts.Run(func(plugin *protogen.Plugin) error {
		// 未指定 domain：按输入的所有 proto 正常逐 service 生成。
		if domain == "" {
			for _, file := range plugin.Files {
				if len(file.Services) == 0 {
					continue
				}
				for _, service := range file.Services {
					if err := generateServiceFile(plugin, file, service); err != nil {
						return err
					}
				}
			}
			return nil
		}

		// 指定 domain：筛选后聚合成一个 domain 文件输出。
		domainFiles := collectDomainFiles(plugin.Files, domain)
		if len(domainFiles) == 0 {
			return nil
		}
		return generateDomainFile(plugin, domain, domainFiles)
	})
}

func collectDomainFiles(files []*protogen.File, domain string) []*protogen.File {
	matched := make([]*protogen.File, 0)
	for _, file := range files {
		if len(file.Services) == 0 {
			continue
		}
		if matchDomain(file.Desc.Path(), domain) {
			matched = append(matched, file)
		}
	}
	return matched
}

func matchDomain(protoPath, domain string) bool {
	d := strings.Trim(domain, "/")
	if d == "" {
		return true
	}
	if protoPath == d+".proto" {
		return true
	}
	return strings.HasPrefix(protoPath, d+"/")
}

func generateDomainFile(plugin *protogen.Plugin, domain string, files []*protogen.File) error {
	d := strings.Trim(domain, "/")
	if d == "" {
		return nil
	}
	// domain 模式固定生成单文件：<domain>.go
	fileName := d + ".go"
	baseImportPath := files[0].GoImportPath

	specs := make([]serviceSpec, 0)
	for _, f := range files {
		for _, s := range f.Services {
			specs = append(specs, serviceSpec{file: f, service: s})
		}
	}

	content, err := buildMergedFileContent(fileName, "source domain: "+d, specs)
	if err != nil {
		return err
	}
	g := plugin.NewGeneratedFile(fileName, baseImportPath)
	_, _ = g.Write([]byte(content))
	return nil
}

func generateServiceFile(plugin *protogen.Plugin, file *protogen.File, service *protogen.Service) error {
	// 默认模式下按 service 拆分文件，方便独立维护。
	fileName := serviceFileName(file.GeneratedFilenamePrefix, service.GoName)
	appImportPath := file.GoImportPath
	content, err := buildMergedFileContent(fileName, "source: "+file.Desc.Path(), []serviceSpec{{file: file, service: service}})
	if err != nil {
		return err
	}
	g := plugin.NewGeneratedFile(fileName, appImportPath)
	_, _ = g.Write([]byte(content))
	return nil
}

func applicationName(serviceGoName string) string {
	base := trimServiceSuffix(serviceGoName)
	return base + "Application"
}

func serviceFileName(generatedPrefix, serviceGoName string) string {
	_ = generatedPrefix
	base := toLowerFirst(trimServiceSuffix(serviceGoName))
	return base + ".go"
}

func trimServiceSuffix(serviceGoName string) string {
	// WelcomeService -> Welcome，避免生成 WelcomeServiceApplication 这种重复后缀。
	if strings.HasSuffix(serviceGoName, "Service") {
		return strings.TrimSuffix(serviceGoName, "Service")
	}
	return serviceGoName
}

func toLowerFirst(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

func unaryMethodImpl(imports *importManager, appName string, method *protogen.Method) string {
	methodName := method.GoName
	inType := "*" + imports.aliasOf(method.Input.GoIdent.GoImportPath) + "." + method.Input.GoIdent.GoName
	outType := "*" + imports.aliasOf(method.Output.GoIdent.GoImportPath) + "." + method.Output.GoIdent.GoName
	outValue := strings.TrimPrefix(outType, "*")
	return fmt.Sprintf("func (app *%s) %s(ctx context.Context, req %s) (%s, error) {\n\t// coding here...\n\n\treturn &%s{}, nil\n}", appName, methodName, inType, outType, outValue)
}

type importManager struct {
	pathAlias map[string]string
	usedAlias map[string]struct{}
}

func newImportManager() *importManager {
	return &importManager{
		pathAlias: make(map[string]string),
		usedAlias: make(map[string]struct{}),
	}
}

func (m *importManager) register(importPath protogen.GoImportPath, preferred string) {
	m.registerWithAlias(string(importPath), "")
	if preferred == "" {
		return
	}
	p := string(importPath)
	if strings.HasSuffix(m.pathAlias[p], "PB") {
		base := sanitizeIdentifier(preferred)
		if base != "" {
			m.forceAlias(p, base+"PB")
		}
	}
}

func (m *importManager) registerWithAlias(importPath, alias string) {
	if importPath == "" {
		return
	}
	if importPath == "context" {
		m.pathAlias[importPath] = ""
		return
	}
	if existing, ok := m.pathAlias[importPath]; ok {
		if alias == "" || existing == alias {
			return
		}
	}
	if alias == "" {
		base := sanitizeIdentifier(path.Base(importPath))
		if base == "" {
			base = "pb"
		}
		alias = base + "PB"
	}
	if _, used := m.usedAlias[alias]; used {
		i := 2
		base := alias
		for {
			candidate := fmt.Sprintf("%s%d", base, i)
			if _, ok := m.usedAlias[candidate]; !ok {
				alias = candidate
				break
			}
			i++
		}
	}
	m.pathAlias[importPath] = alias
	m.usedAlias[alias] = struct{}{}
}

func (m *importManager) forceAlias(importPath, alias string) {
	if importPath == "" || alias == "" {
		return
	}
	if old, ok := m.pathAlias[importPath]; ok && old != "" {
		delete(m.usedAlias, old)
	}
	if _, used := m.usedAlias[alias]; used {
		i := 2
		base := alias
		for {
			candidate := fmt.Sprintf("%s%d", base, i)
			if _, ok := m.usedAlias[candidate]; !ok {
				alias = candidate
				break
			}
			i++
		}
	}
	m.pathAlias[importPath] = alias
	m.usedAlias[alias] = struct{}{}
}

func (m *importManager) aliasOf(importPath protogen.GoImportPath) string {
	p := string(importPath)
	if alias, ok := m.pathAlias[p]; ok {
		return alias
	}
	m.registerWithAlias(p, "")
	return m.pathAlias[p]
}

func (m *importManager) snapshot() map[string]string {
	out := make(map[string]string, len(m.pathAlias))
	for p, a := range m.pathAlias {
		out[p] = a
	}
	return out
}

func sanitizeIdentifier(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i == 0 {
			if unicode.IsLetter(r) || r == '_' {
				b.WriteRune(unicode.ToLower(r))
			}
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

type serviceSpec struct {
	file    *protogen.File
	service *protogen.Service
}

type existingState struct {
	packageName string
	imports     map[string]string
	decls       []string
	hasStruct   map[string]bool
	hasFunc     map[string]bool
	hasMethod   map[string]map[string]bool
}

func buildMergedFileContent(filePath, sourceComment string, specs []serviceSpec) (string, error) {
	existingPath := resolveExistingFilePath(filePath)
	st, exists, err := readExistingState(existingPath)
	if err != nil {
		return "", err
	}
	if !exists {
		st = existingState{
			packageName: "application",
			imports:     map[string]string{},
			decls:       []string{},
			hasStruct:   map[string]bool{},
			hasFunc:     map[string]bool{},
			hasMethod:   map[string]map[string]bool{},
		}
	}

	imports := newImportManager()
	for pathStr, alias := range st.imports {
		imports.registerWithAlias(pathStr, alias)
	}

	addedDecls := make([]string, 0)
	for _, spec := range specs {
		appName := applicationName(spec.service.GoName)
		serverAlias := ensurePBImport(imports, string(spec.file.GoImportPath), string(spec.file.GoPackageName))

		if !st.hasStruct[appName] {
			structDecl := fmt.Sprintf("type %s struct {\n\t%s.Unimplemented%sServer\n}", appName, serverAlias, spec.service.GoName)
			serviceDoc := adaptServiceComment(protoComment(spec.service.Comments), spec.service.GoName, appName)
			addedDecls = append(addedDecls, withDocComment(serviceDoc, structDecl))
			st.hasStruct[appName] = true
		}

		ctorName := "New" + appName
		if !st.hasFunc[ctorName] {
			addedDecls = append(addedDecls, fmt.Sprintf("func %s() *%s {\n\treturn &%s{}\n}", ctorName, appName, appName))
			st.hasFunc[ctorName] = true
		}

		for _, method := range spec.service.Methods {
			if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
				continue
			}
			if hasMethod(st.hasMethod, appName, method.GoName) {
				continue
			}
			imports.registerWithAlias("context", "")
			imports.register(method.Input.GoIdent.GoImportPath, "")
			imports.register(method.Output.GoIdent.GoImportPath, "")
			methodDecl := unaryMethodImpl(imports, appName, method)
			addedDecls = append(addedDecls, withDocComment(protoComment(method.Comments), methodDecl))
			markMethod(st.hasMethod, appName, method.GoName)
		}
	}

	mergedImports := imports.snapshot()
	if exists && len(addedDecls) == 0 && sameImportSet(st.imports, mergedImports) {
		return readOriginalContent(existingPath)
	}

	var buf bytes.Buffer
	buf.WriteString("// Code generated by protoc-gen-go-skeleton.\n")
	buf.WriteString("// " + sourceComment + "\n\n")
	pkg := st.packageName
	if pkg == "" {
		pkg = "application"
	}
	buf.WriteString("package " + pkg + "\n\n")

	paths := make([]string, 0, len(mergedImports))
	for p := range mergedImports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > 0 {
		buf.WriteString("import (\n")
		for _, p := range paths {
			alias := mergedImports[p]
			if alias == "" {
				buf.WriteString("\t" + strconv.Quote(p) + "\n")
			} else {
				buf.WriteString("\t" + alias + " " + strconv.Quote(p) + "\n")
			}
		}
		buf.WriteString(")\n\n")
	}

	allDecls := make([]string, 0, len(st.decls)+len(addedDecls))
	allDecls = append(allDecls, st.decls...)
	allDecls = append(allDecls, addedDecls...)
	for i, d := range allDecls {
		buf.WriteString(strings.TrimSpace(d))
		if i < len(allDecls)-1 {
			buf.WriteString("\n\n")
		}
	}
	buf.WriteString("\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("%s: generated unparsable Go source: %w", filePath, err)
	}
	return string(formatted), nil
}

func resolveExistingFilePath(fileName string) string {
	// 先尝试当前目录，再兼容常见的目标目录（例如 --go-skeleton_out=...:./internal/application）。
	candidates := []string{
		fileName,
		path.Join("internal", "application", fileName),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return fileName
}

func ensurePBImport(imports *importManager, importPath, preferredPkg string) string {
	base := sanitizeIdentifier(preferredPkg)
	if base == "" {
		base = sanitizeIdentifier(path.Base(importPath))
	}
	if base == "" {
		base = "pb"
	}
	target := base + "PB"
	imports.registerWithAlias(importPath, target)
	return imports.pathAlias[importPath]
}

func readExistingState(filePath string) (existingState, bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return existingState{}, false, nil
		}
		return existingState{}, false, err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return existingState{}, true, err
	}

	st := existingState{
		packageName: f.Name.Name,
		imports:     map[string]string{},
		decls:       []string{},
		hasStruct:   map[string]bool{},
		hasFunc:     map[string]bool{},
		hasMethod:   map[string]map[string]bool{},
	}

	for _, is := range f.Imports {
		p, _ := strconv.Unquote(is.Path.Value)
		alias := ""
		if is.Name != nil {
			alias = is.Name.Name
		}
		st.imports[p] = alias
	}

	for _, decl := range f.Decls {
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}

		start := fset.Position(declStart(decl)).Offset
		end := fset.Position(decl.End()).Offset
		if start >= 0 && end > start && end <= len(content) {
			snippet := strings.TrimSpace(string(content[start:end]))
			if snippet != "" {
				st.decls = append(st.decls, snippet)
			}
		}

		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, ok := ts.Type.(*ast.StructType); ok {
					st.hasStruct[ts.Name.Name] = true
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				st.hasFunc[d.Name.Name] = true
				continue
			}
			recv := receiverTypeName(d.Recv.List[0].Type)
			markMethod(st.hasMethod, recv, d.Name.Name)
		}
	}

	return st, true, nil
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func declStart(decl ast.Decl) token.Pos {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil && len(d.Doc.List) > 0 {
			return d.Doc.Pos()
		}
		return d.Pos()
	case *ast.GenDecl:
		if d.Doc != nil && len(d.Doc.List) > 0 {
			return d.Doc.Pos()
		}
		return d.Pos()
	default:
		return decl.Pos()
	}
}

func markMethod(methods map[string]map[string]bool, recv, name string) {
	if recv == "" {
		return
	}
	if _, ok := methods[recv]; !ok {
		methods[recv] = map[string]bool{}
	}
	methods[recv][name] = true
}

func hasMethod(methods map[string]map[string]bool, recv, name string) bool {
	if mm, ok := methods[recv]; ok {
		return mm[name]
	}
	return false
}

func sameImportSet(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func readOriginalContent(filePath string) (string, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func protoComment(cs protogen.CommentSet) string {
	parts := make([]string, 0)
	for _, detached := range cs.LeadingDetached {
		txt := strings.TrimSpace(detached.String())
		if txt != "" {
			parts = append(parts, txt)
		}
	}
	leading := strings.TrimSpace(cs.Leading.String())
	if leading != "" {
		parts = append(parts, leading)
	}
	return strings.Join(parts, "\n")
}

func withDocComment(raw, decl string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return decl
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "//"))
		if line == "" {
			out = append(out, "//")
			continue
		}
		out = append(out, "// "+line)
	}
	return strings.Join(out, "\n") + "\n" + decl
}

func adaptServiceComment(raw, serviceName, appName string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	// .proto 注释常写成 XxxService，这里替换成生成后的 XxxApplication，保证注释与类型名一致。
	return strings.ReplaceAll(raw, serviceName, appName)
}
