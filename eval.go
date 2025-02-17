package runn

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/ast"
	"github.com/antonmedv/expr/file"
	"github.com/antonmedv/expr/parser"
	"github.com/antonmedv/expr/parser/lexer"
	"github.com/goccy/go-json"
	"github.com/goccy/go-yaml"
	"github.com/k1LoW/expand"
	or "github.com/ryo-yamaoka/otchkiss/result"
	"github.com/xlab/treeprint"
)

const (
	delimStart = "{{"
	delimEnd   = "}}"
)

func Eval(e string, store interface{}) (interface{}, error) {
	v, err := expr.Eval(trimComment(e), store)
	if err != nil {
		return nil, fmt.Errorf("eval error: %w", err)
	}
	return v, nil
}

func EvalCond(cond string, store interface{}) (bool, error) {
	v, err := Eval(cond, store)
	if err != nil {
		return false, err
	}
	switch vv := v.(type) {
	case bool:
		return vv, nil
	default:
		return false, nil
	}
}

func EvalCount(count string, store interface{}) (int, error) {
	r, err := Eval(count, store)
	if err != nil {
		return 0, err
	}
	var c int
	switch v := r.(type) {
	case string:
		c, err = strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid count: evaluated %s, but got %T(%v): %w", count, r, r, err)
		}
	case int64:
		c = int(v)
	case float64:
		c = int(v)
	case int:
		c = v
	default:
		return 0, fmt.Errorf("invalid count: evaluated %s, but got %T(%v)", count, r, r)
	}
	return c, nil
}

func EvalExpand(in, store interface{}) (interface{}, error) {
	if s, ok := in.(string); ok {
		if !strings.Contains(s, delimStart) {
			// No need to expand
			return in, nil
		}
		if strings.HasPrefix(s, delimStart) && strings.HasSuffix(s, delimEnd) && strings.Count(s, delimStart) == 1 && strings.Count(s, delimEnd) == 1 {
			// Simple eval since one pair of delims
			return Eval(strings.TrimSuffix(strings.TrimPrefix(s, delimStart), delimEnd), store)
		}
	}
	// Expand using expand.ExprRepFn
	b, err := yaml.Marshal(in)
	if err != nil {
		return nil, err
	}
	e, err := expand.ReplaceYAML(string(b), expand.ExprRepFn(delimStart, delimEnd, store), true)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := yaml.Unmarshal([]byte(e), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func CheckThreshold(threshold string, d time.Duration, r *or.Result) error {
	if threshold == "" {
		return nil
	}
	succeeded := r.Succeeded()
	failed := r.Failed()
	total := succeeded + failed

	max, err := r.PercentileLatency(100)
	if err != nil {
		return err
	}
	min, err := r.PercentileLatency(0)
	if err != nil {
		return err
	}
	p99, err := r.PercentileLatency(99)
	if err != nil {
		return err
	}
	p90, err := r.PercentileLatency(90)
	if err != nil {
		return err
	}
	p50, err := r.PercentileLatency(50)
	if err != nil {
		return err
	}

	ll := r.Latencies()
	var avg float64
	for _, l := range ll {
		avg += l
	}
	avg = avg / float64(len(ll))

	store := map[string]interface{}{
		"total":      total,
		"succeeded":  succeeded,
		"failed":     failed,
		"error_rate": float64(failed) / float64(total) * 100,
		"rps":        float64(total) / d.Seconds(),
		"max":        max * 1000,
		"mid":        p50 * 1000,
		"min":        min * 1000,
		"p90":        p90 * 1000,
		"p99":        p99 * 1000,
		"avg":        avg * 1000,
	}

	tf, err := EvalCond(threshold, store)
	if err != nil {
		return err
	}
	if !tf {
		bt, err := buildTree(threshold, store)
		if err != nil {
			return err
		}
		return fmt.Errorf("(%s) is not true\n%s", threshold, bt)
	}
	return nil
}

func buildTree(cond string, store interface{}) (string, error) {
	if cond == "" {
		return "", nil
	}
	cond = trimComment(cond)
	tree := treeprint.New()
	tree.SetValue(cond)
	vs, err := values(cond)
	if err != nil {
		return "", err
	}
	for _, p := range vs {
		s := strings.Trim(p, " ")
		v, err := Eval(s, store)
		if err != nil {
			tree.AddBranch(fmt.Sprintf("%s => ?", s))
			continue
		}
		if vv, ok := v.(string); ok {
			tree.AddBranch(fmt.Sprintf(`%s => "%s"`, s, vv))
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			tree.AddBranch(fmt.Sprintf("%s => ?", s))
			continue
		}
		tree.AddBranch(fmt.Sprintf("%s => %s", s, string(b)))
	}
	return tree.String(), nil
}

func trimComment(cond string) string {
	const commentToken = "#"
	trimed := []string{}
	for _, l := range strings.Split(cond, "\n") {
		if strings.HasPrefix(strings.Trim(l, " "), commentToken) {
			continue
		}
		s := file.NewSource(l)
		tokens, err := lexer.Lex(s)
		if err != nil {
			trimed = append(trimed, l)
			continue
		}

		ccol := -1
		inClosure := false
	L:
		for _, t := range tokens {
			switch {
			case t.Kind == lexer.Bracket && t.Value == "{":
				inClosure = true
			case t.Kind == lexer.Bracket && t.Value == "}":
				inClosure = false
			case t.Kind == lexer.Operator && t.Value == commentToken && !inClosure:
				ccol = t.Column
				break L
			}
		}
		if ccol > 0 {
			trimed = append(trimed, strings.TrimSuffix(l[:ccol], " "))
			continue
		}

		trimed = append(trimed, l)
	}
	return strings.Join(trimed, "\n")
}

func values(cond string) ([]string, error) {
	t, err := parser.Parse(cond)
	if err != nil {
		return nil, err
	}
	values := nodeValues(t.Node)
	return values, nil
}

func nodeValues(n ast.Node) []string {
	values := []string{}
	switch v := n.(type) {
	case *ast.BinaryNode:
		values = append(values, nodeValues(v.Left)...)
		values = append(values, nodeValues(v.Right)...)
	case *ast.BoolNode:
		values = append(values, fmt.Sprintf(`%v`, v.Value))
	case *ast.StringNode:
		values = append(values, fmt.Sprintf(`"%s"`, v.Value))
	case *ast.IntegerNode:
		values = append(values, fmt.Sprintf(`%d`, v.Value))
	case *ast.FloatNode:
		values = append(values, fmt.Sprintf(`%v`, v.Value))
	case *ast.ArrayNode:
		values = append(values, arrayNode(v))
	case *ast.MapNode:
		values = append(values, mapNode(v))
	case *ast.IdentifierNode:
		values = append(values, v.Value)
	case *ast.PropertyNode:
		values = append(values, propertyNode(v))
	case *ast.IndexNode:
		values = append(values, indexNode(v))
	case *ast.FunctionNode:
		values = append(values, functionNode(v)...)
	case *ast.NilNode:
		values = append(values, fmt.Sprintf(`%v`, nil))
	case *ast.BuiltinNode:
		values = append(values, builtinNode(v)...)
	}
	return values
}

func nodeValue(n ast.Node) string {
	ns := nodeValues(n)
	if len(ns) != 1 {
		return ""
	}
	return ns[0]
}

func arrayNode(a *ast.ArrayNode) string {
	elems := []string{}
	for _, e := range a.Nodes {
		elems = append(elems, nodeValue(e))
	}
	return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
}

func mapNode(m *ast.MapNode) string {
	kvs := []string{}
	for _, p := range m.Pairs {
		switch v := p.(type) {
		case *ast.PairNode:
			kvs = append(kvs, fmt.Sprintf("%s: %s", strings.Trim(nodeValue(v.Key), `"`), nodeValue(v.Value)))
		}
	}
	return fmt.Sprintf("{%s}", strings.Join(kvs, ", "))
}

func propertyNode(p *ast.PropertyNode) string {
	return fmt.Sprintf("%s.%s", nodeValue(p.Node), p.Property)
}

func indexNode(i *ast.IndexNode) string {
	n := nodeValue(i.Node)
	switch v := i.Index.(type) {
	case *ast.StringNode:
		return fmt.Sprintf(`%s["%s"]`, n, v.Value)
	case *ast.IntegerNode:
		return fmt.Sprintf(`%s[%d]`, n, v.Value)
	case *ast.IdentifierNode:
		return fmt.Sprintf(`%s[%s]`, n, v.Value)
	default:
		return ""
	}
}

func functionNode(f *ast.FunctionNode) []string {
	args := []string{}
	for _, a := range f.Arguments {
		args = append(args, nodeValue(a))
	}
	values := []string{fmt.Sprintf("%s(%s)", f.Name, strings.Join(args, ", "))}
	return append(values, args...)
}

func builtinNode(b *ast.BuiltinNode) []string {
	args := []string{}
	for _, a := range b.Arguments {
		args = append(args, nodeValue(a))
	}
	values := []string{fmt.Sprintf("%s(%s)", b.Name, strings.Join(args, ", "))}
	return append(values, args...)
}
