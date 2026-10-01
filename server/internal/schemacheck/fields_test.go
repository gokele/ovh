package schemacheck

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestResponseFieldChains 校验「代码读取的每个响应字段」仍然存在于当前 schema 的响应模型里。
//
// 为什么需要它:TestOVHSchemaDrift 的基线只存响应**类型名**,不存模型**字段清单** ——
// OVH 从某个模型里删掉一个字段,漂移检测是瞎的,而我们的代码会开始静默拿到零值
// (Go 解 JSON 缺字段不报错)。这条测试补上那层:
//
//	AST 提取每个 OVH 调用的响应变量被读了哪些字段链(a.b 形式,含一层别名),
//	对上基线的响应类型,再逐跳走 schema 模型树,任何一跳断掉就报。
//
// 需要联网:OVH_SCHEMA_CHECK=1 go test ./internal/schemacheck/ -run TestResponseFieldChains -v
// 与漂移检测同批跑即可。
func TestResponseFieldChains(t *testing.T) {
	if os.Getenv("OVH_SCHEMA_CHECK") == "" {
		t.Skip("需要联网:设 OVH_SCHEMA_CHECK=1 运行(与 TestOVHSchemaDrift 同批)")
	}

	chains := scanFieldChains(t, "..")
	if len(chains) == 0 {
		t.Fatal("一条字段链都没扫到 —— 提取逻辑坏了,这会静默变成永远通过的空断言")
	}

	// 模型池:全部命名空间(EU)合并。同名模型跨命名空间定义可能不同,
	// 以端点所在命名空间的定义为准 —— 按 namespace 分桶而不是拍平。
	modelsByNS := map[string]map[string]map[string]string{} // ns -> model -> field -> type
	for _, ns := range namespaces {
		doc, err := fetchSchema("EU", ns)
		if err != nil {
			t.Fatalf("拉取 EU/%s 失败: %v", ns, err)
		}
		pool := map[string]map[string]string{}
		raw, _ := doc["models"].(map[string]interface{})
		for name, m := range raw {
			mm, _ := m.(map[string]interface{})
			props, _ := mm["properties"].(map[string]interface{})
			fields := map[string]string{}
			for f, pv := range props {
				p, _ := pv.(map[string]interface{})
				ty, _ := p["type"].(string)
				fields[f] = ty
			}
			pool[name] = fields
		}
		modelsByNS[ns] = pool
	}

	// 端点 → 响应类型(基线) + 端点 → 命名空间(路径前缀推断)
	respType := map[string]string{}
	for k, v := range baselineData {
		if e, ok := v["EU"]; ok {
			if s, ok := e.(map[string]interface{})["response"].(string); ok {
				respType[k] = s
			}
		}
	}

	checked, skipped, broken := 0, 0, []string{}
	for _, ch := range chains {
		cands := matchEndpoint(ch.method, ch.path)
		if len(cands) == 0 {
			skipped++
			continue
		}
	passAny:
		for _, cand := range cands {
			rt := respType[cand]
			if err := walkChain(rt, ch.field, nsOf(cand), modelsByNS); err == nil {
				checked++
				break passAny
			} else if len(cands) == 1 {
				broken = append(broken, ch.method+" "+cand+" | 链 "+ch.field+" (响应 "+rt+") → "+err.Error())
				break passAny
			}
		}
	}
	sort.Strings(broken)
	if len(broken) > 0 {
		t.Errorf("这 %d 条字段链在当前 schema 模型里断了 —— OVH 可能删/改了字段,\n"+
			"代码会静默拿到零值,请逐条核对:\n  %s", len(broken), strings.Join(broken, "\n  "))
	}
	t.Logf("字段链核对: %d 通过, %d 组未匹配端点(裸HTTP/非OVH), %d 断链", checked, skipped, len(broken))
}

// ─────────────── 提取侧 ───────────────

type fieldChain struct {
	method string
	path   string // pathTemplate 归一(变量段是 {})
	field  string // a.b 点链
}

// scanFieldChains 找 client.Xxx(path, ..., &out) 里的 out,再收 out 及其一层别名的 ["k"] 读取。
func scanFieldChains(t *testing.T, root string) []fieldChain {
	t.Helper()
	var res []fieldChain

	_, funcs, _ := parseFuncs(t, root)
	for fd := range funcs {
		outVars := map[string][2]string{} // var -> {tplPath, METHOD} —— 每个函数独立,别跨函数串
		aliases := map[string]string{}    // aliasVar -> "parent\x00field"
		reads := map[string]map[string]bool{}

		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok || len(ce.Args) == 0 {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			m, ok := clientMethods[sel.Sel.Name]
			if !ok {
				return true
			}
			pi := 0
			if strings.HasSuffix(sel.Sel.Name, "WithContext") {
				pi = 1
			}
			if pi >= len(ce.Args) {
				return true
			}
			tpl := pathTemplate(ce.Args[pi])
			if !strings.HasPrefix(tpl, "/") {
				return true
			}
			if i := strings.Index(tpl, "?"); i >= 0 {
				tpl = tpl[:i]
			}
			var vn string
			if len(ce.Args) > pi+1 {
				last := ce.Args[len(ce.Args)-1]
				if u, ok := last.(*ast.UnaryExpr); ok && u.Op.String() == "&" {
					if id, ok := u.X.(*ast.Ident); ok {
						vn = id.Name
					}
				} else if id, ok := last.(*ast.Ident); ok {
					vn = id.Name
				}
			}
			if vn == "" {
				return true
			}
			outVars[vn] = [2]string{tpl, m}
			return true
		})
		if len(outVars) == 0 {
			continue
		}
		// 一层别名:x, _ := v["k"].(map[string]interface{})
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) == 0 || len(as.Rhs) == 0 {
				return true
			}
			id, ok := as.Lhs[0].(*ast.Ident)
			if !ok {
				return true
			}
			ta, ok := as.Rhs[0].(*ast.TypeAssertExpr)
			if !ok {
				return true
			}
			idx, ok := ta.X.(*ast.IndexExpr)
			if !ok {
				return true
			}
			base, ok := idx.X.(*ast.Ident)
			if !ok {
				return true
			}
			if _, isOut := outVars[base.Name]; !isOut {
				return true
			}
			if k, ok := litStr(idx.Index); ok {
				aliases[id.Name] = base.Name + "\x00" + k
			}
			return true
		})
		// 字段读取,两种形态:
		//   ① v["k"] / v["a"].(map)["b"] / alias["k"]
		//   ② valueOr(v, "k", …) —— 本仓库惯用的带兜底读法,只认 IndexExpr 会漏掉一大片
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			record := func(rootIdent, chain, key string) {
				if rootIdent == "" {
					return
				}
				if _, isOut := outVars[rootIdent]; isOut {
					reads[rootIdent] = addRead(reads[rootIdent], joinDot(chain, key))
				} else if a, isAl := aliases[rootIdent]; isAl {
					parts := strings.SplitN(a, "\x00", 2)
					if _, isOut := outVars[parts[0]]; isOut {
						reads[parts[0]] = addRead(reads[parts[0]], joinDot(parts[1], joinDot(chain, key)))
					}
				}
			}
			if ce, ok := n.(*ast.CallExpr); ok && len(ce.Args) >= 2 {
				if sel, ok := ce.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "valueOr" {
					if id, ok := ce.Args[0].(*ast.Ident); ok {
						if k, ok := litStr(ce.Args[1]); ok {
							record(id.Name, "", k)
						}
					}
					return true
				}
			}
			idx, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			k, ok := litStr(idx.Index)
			if !ok {
				return true
			}
			var root, chain string
			switch x := idx.X.(type) {
			case *ast.Ident:
				root = x.Name
			case *ast.TypeAssertExpr:
				if idx2, ok := x.X.(*ast.IndexExpr); ok {
					if id, ok := idx2.X.(*ast.Ident); ok {
						root = id.Name
						if kk, ok := litStr(idx2.Index); ok {
							chain = kk
						}
					}
				}
			}
			record(root, chain, k)
			return true
		})

		for v, fs := range reads {
			e := outVars[v]
			for f := range fs {
				res = append(res, fieldChain{method: e[1], path: e[0], field: f})
			}
		}
	}
	return res
}

func addRead(m map[string]bool, f string) map[string]bool {
	if m == nil {
		m = map[string]bool{}
	}
	m[f] = true
	return m
}

func joinDot(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func litStr(e ast.Expr) (string, bool) {
	if bl, ok := e.(*ast.BasicLit); ok && len(bl.Value) >= 2 && bl.Value[0] == '"' {
		return bl.Value[1 : len(bl.Value)-1], true
	}
	return "", false
}

// ─────────────── schema 侧 ───────────────

// matchEndpoint 把提取的模板路径({} 变量段)匹配到基线 key(参数段 {x} 与 {} 等价)。
func matchEndpoint(method, tpl string) []string {
	es := strings.FieldsFunc(tpl, func(r rune) bool { return r == '/' })
	var out []string
	for k := range baselineData {
		m, p, ok := strings.Cut(k, " ")
		if !ok || m != method {
			continue
		}
		bs := strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
		if len(bs) != len(es) {
			continue
		}
		ok2 := true
		for i := range es {
			if strings.HasPrefix(es[i], "{") || strings.HasPrefix(bs[i], "{") || es[i] == bs[i] {
				continue
			}
			ok2 = false
			break
		}
		if ok2 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// nsOf 从端点路径前缀推断命名空间(取 schema 的 models 桶)
func nsOf(endpoint string) string {
	p := strings.TrimPrefix(strings.SplitN(endpoint, " ", 2)[1], "/")
	segs := strings.SplitN(p, "/", 3)
	if len(segs) >= 2 && segs[0] == "dedicated" {
		return "dedicated/" + segs[1]
	}
	return segs[0]
}

// walkChain 在响应类型上逐跳验证字段链,断链返回原因。
func walkChain(respType, chain, ns string, modelsByNS map[string]map[string]map[string]string) error {
	pool := modelsByNS[ns]
	t := normalizeType(respType)
	if t == "" {
		return nil // 原始类型/自由 map,没有字段层可核
	}
	for i, seg := range strings.Split(chain, ".") {
		props, ok := pool[t]
		if !ok {
			return errStr("模型 " + t + " 不在 " + ns + " 的 models 里")
		}
		ty, ok := props[seg]
		if !ok {
			return errStr(t + " 没有字段 " + seg)
		}
		_ = i
		t = normalizeType(ty)
		if t == "" {
			return nil // 后续是原始值,链到此为止(更深的跳由解析层兜住,不算断)
		}
	}
	return nil
}

func normalizeType(t string) string {
	t = strings.TrimSuffix(strings.TrimSpace(t), "[]")
	if t == "" || strings.HasPrefix(t, "map[") {
		return ""
	}
	switch t {
	case "long", "string", "boolean", "double", "datetime", "json", "void", "null", "object", "any":
		return ""
	}
	return t
}

type errStr string

func (e errStr) Error() string { return string(e) }

// baselineData TestOVHSchemaDrift 的基线产物(testdata/ovh-endpoints.json),懒加载。
var baselineData = func() map[string]map[string]interface{} {
	b, err := os.ReadFile("testdata/ovh-endpoints.json")
	if err != nil {
		return map[string]map[string]interface{}{}
	}
	var m map[string]map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return map[string]map[string]interface{}{}
	}
	return m
}()

// parseFuncs 解析源码树,返回全部函数体(遍历口径与 usage_test.go 一致)
func parseFuncs(t *testing.T, root string) (*token.FileSet, map[*ast.FuncDecl]bool, error) {
	t.Helper()
	fset := token.NewFileSet()
	funcs := map[*ast.FuncDecl]bool{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() ||
			!strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				funcs[fd] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}
	return fset, funcs, err
}
