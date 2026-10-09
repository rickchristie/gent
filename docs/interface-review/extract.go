//go:build ignore

// Extract the checkout's Go contracts with the standard parser. Documentation must not depend
// on an index maintained by hand: adding an interface must make the review build demand copy.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type method struct {
	Name string `json:"name"`
	Code string `json:"code"`
	Doc  string `json:"doc"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type declaration struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Package  string   `json:"package"`
	Kind     string   `json:"kind"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Code     string   `json:"code"`
	FullCode string   `json:"fullCode"`
	Doc      string   `json:"doc"`
	Methods  []method `json:"methods"`
	Embeds   []string `json:"embeds"`
	Change   string   `json:"change"`
	Previous string   `json:"previous"`
}

type catalog struct {
	Head         string        `json:"head"`
	Fingerprint  string        `json:"fingerprint"`
	Files        int           `json:"files"`
	Declarations []declaration `json:"declarations"`
	Capabilities []method      `json:"capabilities"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run extract.go <repository>")
	}
	root := os.Args[1]
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	tracked, err := git(root, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return err
	}
	paths := map[string]bool{}
	for _, path := range strings.Split(tracked, "\n") {
		paths[path] = true
	}
	result := catalog{Head: head}
	digest := sha256.New()
	allMethods := map[string][]method{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") ||
				entry.Name() == "vendor" || entry.Name() == "node_modules" ||
				entry.Name() == "docs") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest.Write([]byte(relative + "\x00"))
		digest.Write(source)
		result.Files++
		decls, methods, err := parse(relative, source)
		if err != nil {
			return err
		}
		capabilities, err := parseCapabilities(relative, source)
		if err != nil {
			return err
		}
		result.Capabilities = append(result.Capabilities, capabilities...)
		old := map[string]declaration{}
		if paths[relative] {
			previous, err := git(root, "show", "HEAD:"+relative)
			if err != nil {
				return err
			}
			previousDecls, _, err := parse(relative, []byte(previous))
			if err != nil {
				return err
			}
			for _, item := range previousDecls {
				old[item.ID] = item
			}
		}
		for _, item := range decls {
			item.Change = "new"
			if previous, ok := old[item.ID]; ok {
				item.Change = "same"
				if item.Code != previous.Code {
					item.Change = "changed"
					item.Previous = previous.Code
				}
			}
			result.Declarations = append(result.Declarations, item)
		}
		for id, methods := range methods {
			allMethods[id] = append(allMethods[id], methods...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := range result.Declarations {
		item := &result.Declarations[i]
		if item.Kind != "interface" {
			item.Methods = allMethods[item.ID]
		}
	}
	sort.Slice(result.Declarations, func(i, j int) bool {
		return result.Declarations[i].ID < result.Declarations[j].ID
	})
	result.Fingerprint = hex.EncodeToString(digest.Sum(nil))[:16]
	return json.NewEncoder(os.Stdout).Encode(result)
}

func git(root string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

func parse(file string, source []byte) ([]declaration, map[string][]method, error) {
	positions := token.NewFileSet()
	tree, err := parser.ParseFile(positions, file, source, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	plainPositions := token.NewFileSet()
	plainTree, err := parser.ParseFile(plainPositions, file, source, 0)
	if err != nil {
		return nil, nil, err
	}
	plain := map[string]string{}
	for _, node := range plainTree.Decls {
		group, ok := node.(*ast.GenDecl)
		if !ok || group.Tok != token.TYPE {
			continue
		}
		for _, spec := range group.Specs {
			typ := spec.(*ast.TypeSpec)
			plain[typ.Name.Name] = "type " + printNode(plainPositions, typ)
		}
	}
	packageName := tree.Name.Name
	prefix := filepath.ToSlash(filepath.Dir(file))
	if prefix == "." {
		prefix = "gent"
	}
	var declarations []declaration
	methods := map[string][]method{}
	for _, node := range tree.Decls {
		if fn, ok := node.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.IsExported() {
			receiver := receiverName(fn.Recv.List[0].Type)
			id := prefix + "." + receiver
			start := positions.Position(fn.Pos()).Offset
			end := positions.Position(fn.Body.Pos()).Offset
			code := strings.TrimSpace(string(source[start:end]))
			methods[id] = append(methods[id], method{Name: fn.Name.Name, Code: code,
				Doc: fn.Doc.Text(), File: file, Line: positions.Position(fn.Pos()).Line})
			continue
		}
		group, ok := node.(*ast.GenDecl)
		if !ok || group.Tok != token.TYPE {
			continue
		}
		for _, spec := range group.Specs {
			typ := spec.(*ast.TypeSpec)
			if !typ.Name.IsExported() {
				continue
			}
			doc := typ.Doc.Text()
			if doc == "" {
				doc = group.Doc.Text()
			}
			item := declaration{ID: prefix + "." + typ.Name.Name, Name: typ.Name.Name,
				Package: packageName, File: file, Line: positions.Position(typ.Pos()).Line,
				Code: plain[typ.Name.Name], Kind: "type", Doc: doc}
			start := positions.Position(typ.Pos()).Offset
			end := positions.Position(typ.End()).Offset
			item.FullCode = "type " + string(source[start:end])
			if face, ok := typ.Type.(*ast.InterfaceType); ok {
				item.Kind = "interface"
				for _, field := range face.Methods.List {
					if len(field.Names) == 0 {
						item.Embeds = append(item.Embeds, printNode(positions, field.Type))
						continue
					}
					item.Methods = append(item.Methods, method{Name: field.Names[0].Name,
						Code: field.Names[0].Name + strings.TrimPrefix(
							printNode(positions, field.Type), "func"),
						Doc: field.Doc.Text(), File: file,
						Line: positions.Position(field.Pos()).Line})
				}
			}
			declarations = append(declarations, item)
		}
	}
	return declarations, methods, nil
}

func receiverName(node ast.Expr) string {
	switch value := node.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverName(value.X)
	case *ast.IndexExpr:
		return receiverName(value.X)
	case *ast.IndexListExpr:
		return receiverName(value.X)
	default:
		return ""
	}
}

func parseCapabilities(file string, source []byte) ([]method, error) {
	positions := token.NewFileSet()
	tree, err := parser.ParseFile(positions, file, source, 0)
	if err != nil {
		return nil, err
	}
	named := map[*ast.InterfaceType]bool{}
	ast.Inspect(tree, func(node ast.Node) bool {
		if spec, ok := node.(*ast.TypeSpec); ok {
			if face, ok := spec.Type.(*ast.InterfaceType); ok {
				named[face] = true
			}
		}
		return true
	})
	var results []method
	ast.Inspect(tree, func(node ast.Node) bool {
		face, ok := node.(*ast.InterfaceType)
		if !ok || named[face] || len(face.Methods.List) == 0 {
			return true
		}
		var names []string
		for _, field := range face.Methods.List {
			for _, name := range field.Names {
				names = append(names, name.Name)
			}
		}
		results = append(results, method{Name: strings.Join(names, ", "),
			Code: printNode(positions, face), File: file, Line: positions.Position(face.Pos()).Line})
		return true
	})
	return results, nil
}

func printNode(positions *token.FileSet, node any) string {
	var output bytes.Buffer
	if err := format.Node(&output, positions, node); err != nil {
		panic(err)
	}
	return output.String()
}
