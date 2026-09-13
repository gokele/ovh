package schemacheck

// 漂移监控的覆盖率自检。
//
// usedEndpoints 原本是手工维护的,理由写在那儿:"自动扫路径拼接不可靠
// (路径是 "+svc+" 拼出来的),而漏掉一个端点比多写一个危险得多"。
// 前半句对正则成立,对 AST 不成立 —— 拼接表达式在语法树里是完整的 BinaryExpr,
// 把字面量留下、变量换成 {} 就能还原出模板。
//
// 后半句正是实际发生的事:清单只覆盖了代码实际调用的一半多一点。
// POST /me/order/{orderId}/retraction、整套 features/backupFTP、ola/aggregation、
// VPS 的 reinstall/images/tasks —— 这些当时都不在清单里,
// OVH 改了它们的签名不会有任何人知道。
//
// 这条测试不联网,每次 go test 都跑:代码里新增一个端点却忘了纳入漂移监控,
// 当场失败并把该补的行打出来。
import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var paramRe = regexp.MustCompile(`\{[^}]*\}`)

// normEndpoint "GET /vps/{serviceName}/ips" → "GET /vps/{}/ips"
func normEndpoint(s string) string { return paramRe.ReplaceAllString(s, "{}") }

var clientMethods = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"GetWithContext": "GET", "PostWithContext": "POST",
	"PutWithContext": "PUT", "DeleteWithContext": "DELETE",
}

func unquote(e ast.Expr) (string, bool) {
	l, ok := e.(*ast.BasicLit)
	if !ok || l.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(l.Value)
	return s, err == nil
}

// verbsToBraces 把 Sprintf 的动词换成 {},%% 还原成 %
func verbsToBraces(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+1 < len(s) {
			if s[i+1] == '%' {
				b.WriteByte('%')
				i++
				continue
			}
			j := i + 1
			for j < len(s) && strings.ContainsRune("+-# 0123456789.", rune(s[j])) {
				j++
			}
			if j < len(s) {
				b.WriteString("{}")
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// pathTemplate 还原路径表达式。字面量保留,其余一律 {}。
func pathTemplate(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if s, ok := unquote(x); ok {
			return s
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return pathTemplate(x.X) + pathTemplate(x.Y)
		}
	case *ast.ParenExpr:
		return pathTemplate(x.X)
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok &&
			sel.Sel.Name == "Sprintf" && len(x.Args) > 0 {
			if s, ok := unquote(x.Args[0]); ok {
				return verbsToBraces(s)
			}
		}
	}
	return "{}"
}

type siteRef struct{ file string }

// scanCalls 扫出所有 OVH 客户端调用的端点。
// 判据是"第一个参数还原后以 / 开头" —— 这一条就把 http.Client.Get 之类全滤掉了。
func scanCalls(t *testing.T, root string) map[string]siteRef {
	t.Helper()
	found := map[string]siteRef{}
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() ||
			!strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", p, perr)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// path := "..." / path := fmt.Sprintf(...) 之后再 client.Get(path, ...)
			// 是很常见的写法,不解析这一层会整条漏掉。
			strVars := map[string]string{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, l := range as.Lhs {
					id, ok := l.(*ast.Ident)
					if !ok || i >= len(as.Rhs) {
						continue
					}
					if tpl := pathTemplate(as.Rhs[i]); strings.HasPrefix(tpl, "/") {
						strVars[id.Name] = tpl
					}
				}
				return true
			})
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
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
					pi = 1 // 第一个参数是 ctx
				}
				if pi >= len(ce.Args) {
					return true
				}
				tpl := pathTemplate(ce.Args[pi])
				if !strings.HasPrefix(tpl, "/") {
					if id, ok := ce.Args[pi].(*ast.Ident); ok {
						tpl = strVars[id.Name]
					}
				}
				if !strings.HasPrefix(tpl, "/") {
					return true
				}
				if i := strings.Index(tpl, "?"); i >= 0 {
					tpl = tpl[:i] // query string 不属于端点标识
				}
				tpl = strings.TrimRight(tpl, "/")
				if tpl == "" {
					return true
				}
				key := m + " " + tpl
				if _, dup := found[key]; !dup {
					pos := fset.Position(ce.Pos())
					found[key] = siteRef{fmt.Sprintf("%s:%d", pos.Filename, pos.Line)}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历源码失败: %v", err)
	}
	return found
}

func TestDriftListCoversEveryCalledEndpoint(t *testing.T) {
	called := scanCalls(t, "..")
	if len(called) < 100 {
		// 扫出来的数量塌方通常意味着提取逻辑坏了(比如改了客户端方法名),
		// 而那会让这条测试静默变成一条永远通过的空断言。
		t.Fatalf("只扫到 %d 个端点,远少于预期 —— 提取逻辑可能已失效", len(called))
	}
	listed := map[string]bool{}
	for _, e := range usedEndpoints {
		listed[normEndpoint(e)] = true
	}
	missing := []string{}
	for k, ref := range called {
		if !listed[normEndpoint(k)] {
			missing = append(missing, fmt.Sprintf("  %-60s  %s", k, ref.file))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("这 %d 个端点代码在调用,但不在 usedEndpoints 里 —— "+
			"它们的签名漂移不会被 TestOVHSchemaDrift 发现。\n"+
			"把它们按官方路径拼写(带 {serviceName} 这类真实参数名)加进 usedEndpoints:\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
}

// 反向只报告不失败:usedEndpoints 里有、AST 扫不到的,多半是绕过 OVH 客户端的
// 裸 HTTP 调用(目录、VPS 机房可用性都走公开 URL),留着监控是对的。
func TestDriftListExtrasAreReported(t *testing.T) {
	called := map[string]bool{}
	for k := range scanCalls(t, "..") {
		called[normEndpoint(k)] = true
	}
	extras := []string{}
	for _, e := range usedEndpoints {
		if !called[normEndpoint(e)] {
			extras = append(extras, e)
		}
	}
	sort.Strings(extras)
	if len(extras) > 0 {
		t.Logf("usedEndpoints 里这 %d 个端点 AST 扫不到(裸 HTTP 调用或已停用),仅提示:\n  %s",
			len(extras), strings.Join(extras, "\n  "))
	}
}
