// Command check_go verifies public Go documentation and repository source layout.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	if err := check(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(root string) error {
	if err := checkLayout(root); err != nil {
		return err
	}
	fs := token.NewFileSet()
	total, missing := 0, 0
	checkComment := func(name string, pos token.Pos, comment *ast.CommentGroup) {
		total++
		if comment == nil || strings.TrimSpace(comment.Text()) == "" {
			fmt.Fprintf(os.Stderr, "%s: missing public API comment for %s\n", fs.Position(pos), name)
			missing++
		}
	}
	for _, dir := range []string{"kvp", "owscore", "wms", "wmts", "response", "server", "exception", "api", "common", "features", "tiles", "maps", "coverages", "processes"} {
		paths, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return fmt.Errorf("no source files in %s", dir)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Recv != nil && !ast.IsExported(receiver(d.Recv.List[0].Type)) {
						continue
					}
					checkComment(d.Name.Name, d.Pos(), d.Doc)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if s, ok := spec.(*ast.TypeSpec); ok && s.Name.IsExported() {
							comment := s.Doc
							if comment == nil {
								comment = d.Doc
							}
							checkComment(s.Name.Name, s.Pos(), comment)
						}
					}
				}
			}
		}
	}
	if missing > 0 {
		return fmt.Errorf("%d of %d public declarations lack documentation", missing, total)
	}
	fmt.Printf("All %d public type/function/method declarations have GoDoc.\n", total)
	return nil
}

func checkLayout(root string) error {
	files := token.NewFileSet()
	names := regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*\.go$`)
	receivers := make(map[string]string)
	var problems []string
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "testdata" || entry.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if ast.IsGenerated(file) {
			return nil
		}
		count++
		if !names.MatchString(entry.Name()) {
			problems = append(problems, path+": Go filename must use snake_case")
		}
		isTest := strings.HasSuffix(path, "_test.go")
		if isTest {
			counterpart := strings.TrimSuffix(path, "_test.go") + ".go"
			info, err := os.Stat(counterpart)
			if err != nil || !info.Mode().IsRegular() {
				problems = append(problems, path+": test must match a source file in the same directory")
			}
		}
		pastConstants, pastCases := false, false
		for _, declaration := range file.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok {
				if group.Tok == token.IMPORT {
					continue
				}
				if group.Tok == token.CONST {
					if pastConstants {
						problems = append(problems, files.Position(group.Pos()).String()+": constants must precede other declarations")
					}
					continue
				}
			}
			pastConstants = true
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				pastCases = true
				continue
			}
			position := files.Position(function.Pos()).String()
			if isTest && function.Recv == nil && testEntry(function.Name.Name) {
				if pastCases {
					problems = append(problems, position+": test cases must precede helper declarations")
				}
				continue
			}
			pastCases = true
			if parameter := helperParameter(function); parameter != "" && !beginsWithHelper(function, parameter) {
				problems = append(problems, position+": test helper must start with "+parameter+".Helper()")
			}
			if function.Recv == nil || len(function.Recv.List[0].Names) == 0 {
				continue
			}
			receiverField := function.Recv.List[0]
			name := receiverField.Names[0].Name
			if name == "_" {
				continue
			}
			key := filepath.Dir(path) + "/" + file.Name.Name + "." + receiver(receiverField.Type)
			if previous, exists := receivers[key]; exists && previous != name {
				problems = append(problems, position+": inconsistent receiver names for "+key)
			}
			receivers[key] = name
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(problems) != 0 {
		sort.Strings(problems)
		return fmt.Errorf("go layout violations:\n%s", strings.Join(problems, "\n"))
	}
	fmt.Printf("Go layout checked: %d files; source/test names, declaration order, helpers and receivers.\n", count)
	return nil
}

func testEntry(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if remainder, ok := strings.CutPrefix(name, prefix); ok {
			return remainder == "" || !unicode.IsLower([]rune(remainder)[0])
		}
	}
	return false
}

func helperParameter(function *ast.FuncDecl) string {
	for _, field := range function.Type.Params.List {
		typ := field.Type
		if pointer, ok := typ.(*ast.StarExpr); ok {
			typ = pointer.X
		}
		selector, ok := typ.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "testing" || (selector.Sel.Name != "T" && selector.Sel.Name != "B" && selector.Sel.Name != "F" && selector.Sel.Name != "TB") {
			continue
		}
		if len(field.Names) > 0 {
			return field.Names[0].Name
		}
	}
	return ""
}

func beginsWithHelper(function *ast.FuncDecl, parameter string) bool {
	if function.Body == nil || len(function.Body.List) == 0 {
		return false
	}
	statement, ok := function.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := statement.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Helper" {
		return false
	}
	name, ok := selector.X.(*ast.Ident)
	return ok && name.Name == parameter
}

func receiver(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiver(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiver(e.X)
	case *ast.IndexListExpr:
		return receiver(e.X)
	}
	return ""
}
