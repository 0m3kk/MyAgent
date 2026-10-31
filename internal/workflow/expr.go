package workflow

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// StepView is what expressions see for a completed step.
type StepView struct {
	Output any    `json:"output"`
	Status string `json:"status"`
}

// Env is the expression evaluation scope. Secrets are deliberately absent:
// they only exist at the tool execution boundary.
type Env struct {
	Inputs  map[string]any
	Steps   map[string]StepView
	Item    any
	Index   int
	HasItem bool
	Run     map[string]any
	Vars    map[string]any
}

func (e *Env) lookup(name string) (any, bool) {
	switch name {
	case "inputs":
		return e.Inputs, true
	case "steps":
		return stepViewsToMap(e.Steps), true
	case "item":
		if e.HasItem {
			return e.Item, true
		}
		return nil, false
	case "index":
		if e.HasItem {
			return float64(e.Index), true
		}
		return nil, false
	case "run":
		return e.Run, true
	}
	if e.Vars != nil {
		v, ok := e.Vars[name]
		return v, ok
	}
	return nil, false
}

func stepViewsToMap(steps map[string]StepView) map[string]any {
	out := make(map[string]any, len(steps))
	for id, v := range steps {
		out[id] = map[string]any{"output": v.Output, "status": v.Status}
	}
	return out
}

// Ref is a statically discovered variable reference.
type Ref struct {
	Root string
	Path []string
}

type node interface {
	eval(*Env) (any, error)
	refs() []Ref
}

type litNode struct{ value any }

func (n *litNode) eval(*Env) (any, error) { return n.value, nil }
func (n *litNode) refs() []Ref            { return nil }

type identNode struct{ name string }

func (n *identNode) eval(env *Env) (any, error) {
	v, ok := env.lookup(n.name)
	if !ok {
		return nil, fmt.Errorf("unknown variable %q", n.name)
	}
	return v, nil
}
func (n *identNode) refs() []Ref { return []Ref{{Root: n.name}} }

type unaryNode struct {
	op   string
	expr node
}

func (n *unaryNode) eval(env *Env) (any, error) {
	v, err := n.expr.eval(env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "!":
		return !truthy(v), nil
	case "-":
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("unary - expects a number")
		}
		return -f, nil
	}
	return nil, fmt.Errorf("unknown operator %s", n.op)
}
func (n *unaryNode) refs() []Ref { return n.expr.refs() }

type binaryNode struct {
	op          string
	left, right node
}

func (n *binaryNode) eval(env *Env) (any, error) {
	if n.op == "&&" || n.op == "||" {
		lv, err := n.left.eval(env)
		if err != nil {
			return nil, err
		}
		if n.op == "&&" && !truthy(lv) {
			return false, nil
		}
		if n.op == "||" && truthy(lv) {
			return true, nil
		}
		rv, err := n.right.eval(env)
		if err != nil {
			return nil, err
		}
		return truthy(rv), nil
	}
	lv, err := n.left.eval(env)
	if err != nil {
		return nil, err
	}
	rv, err := n.right.eval(env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return equalJSON(lv, rv), nil
	case "!=":
		return !equalJSON(lv, rv), nil
	case "<", "<=", ">", ">=":
		return compare(n.op, lv, rv)
	case "in":
		return containsValue(rv, lv), nil
	case "+":
		if _, ok := lv.(string); ok {
			return fmt.Sprint(lv) + fmt.Sprint(rv), nil
		}
		if _, ok := rv.(string); ok {
			return fmt.Sprint(lv) + fmt.Sprint(rv), nil
		}
		lf, lok := toFloat(lv)
		rf, rok := toFloat(rv)
		if !lok || !rok {
			return nil, fmt.Errorf("+ expects numbers or strings")
		}
		return lf + rf, nil
	case "-", "*", "/", "%":
		lf, lok := toFloat(lv)
		rf, rok := toFloat(rv)
		if !lok || !rok {
			return nil, fmt.Errorf("%s expects numbers", n.op)
		}
		switch n.op {
		case "-":
			return lf - rf, nil
		case "*":
			return lf * rf, nil
		case "/":
			if rf == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			return lf / rf, nil
		default:
			if rf == 0 {
				return nil, fmt.Errorf("modulo by zero")
			}
			return math.Mod(lf, rf), nil
		}
	}
	return nil, fmt.Errorf("unknown operator %s", n.op)
}
func (n *binaryNode) refs() []Ref { return append(n.left.refs(), n.right.refs()...) }

type ternaryNode struct {
	cond, yes, no node
}

func (n *ternaryNode) eval(env *Env) (any, error) {
	c, err := n.cond.eval(env)
	if err != nil {
		return nil, err
	}
	if truthy(c) {
		return n.yes.eval(env)
	}
	return n.no.eval(env)
}
func (n *ternaryNode) refs() []Ref {
	out := n.cond.refs()
	out = append(out, n.yes.refs()...)
	out = append(out, n.no.refs()...)
	return out
}

type memberNode struct {
	base node
	name string
}

func (n *memberNode) eval(env *Env) (any, error) {
	b, err := n.base.eval(env)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, nil
	}
	switch v := b.(type) {
	case map[string]any:
		return v[n.name], nil
	case map[string]StepView:
		return v[n.name], nil
	case StepView:
		switch n.name {
		case "output":
			return v.Output, nil
		case "status":
			return v.Status, nil
		}
		return nil, nil
	}
	return nil, fmt.Errorf("cannot access %q on %T", n.name, b)
}
func (n *memberNode) refs() []Ref {
	out := n.base.refs()
	for i := range out {
		out[i].Path = append(out[i].Path, n.name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type indexNode struct {
	base node
	idx  node
}

func (n *indexNode) eval(env *Env) (any, error) {
	b, err := n.base.eval(env)
	if err != nil {
		return nil, err
	}
	idx, err := n.idx.eval(env)
	if err != nil {
		return nil, err
	}
	switch v := b.(type) {
	case []any:
		f, ok := toFloat(idx)
		if !ok {
			return nil, fmt.Errorf("array index must be a number")
		}
		i := int(f)
		if i < 0 || i >= len(v) {
			return nil, nil
		}
		return v[i], nil
	case map[string]any:
		return v[fmt.Sprint(idx)], nil
	}
	return nil, fmt.Errorf("cannot index %T", b)
}
func (n *indexNode) refs() []Ref {
	out := n.base.refs()
	if lit, ok := n.idx.(*litNode); ok {
		for i := range out {
			out[i].Path = append(out[i].Path, fmt.Sprint(lit.value))
		}
	}
	return out
}

type callNode struct {
	name string
	args []node
}

func (n *callNode) eval(env *Env) (any, error) {
	args := make([]any, len(n.args))
	for i, a := range n.args {
		v, err := a.eval(env)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	switch n.name {
	case "size", "len":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() takes one argument", n.name)
		}
		switch v := args[0].(type) {
		case string:
			return float64(len([]rune(v))), nil
		case []any:
			return float64(len(v)), nil
		case map[string]any:
			return float64(len(v)), nil
		case nil:
			return float64(0), nil
		}
		return nil, fmt.Errorf("size() does not support %T", args[0])
	case "string":
		if len(args) != 1 {
			return nil, fmt.Errorf("string() takes one argument")
		}
		if args[0] == nil {
			return "", nil
		}
		if s, ok := args[0].(string); ok {
			return s, nil
		}
		b, _ := json.Marshal(args[0])
		return string(b), nil
	case "int":
		if len(args) != 1 {
			return nil, fmt.Errorf("int() takes one argument")
		}
		f, ok := toFloat(args[0])
		if !ok {
			s, _ := args[0].(string)
			parsed, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("cannot convert to int")
			}
			return float64(parsed), nil
		}
		return math.Trunc(f), nil
	case "float":
		f, ok := toFloat(args[0])
		if !ok || len(args) != 1 {
			return nil, fmt.Errorf("cannot convert to float")
		}
		return f, nil
	case "bool":
		if len(args) != 1 {
			return nil, fmt.Errorf("bool() takes one argument")
		}
		return truthy(args[0]), nil
	case "lower":
		return strings.ToLower(fmt.Sprint(args[0])), nil
	case "upper":
		return strings.ToUpper(fmt.Sprint(args[0])), nil
	case "trim":
		return strings.TrimSpace(fmt.Sprint(args[0])), nil
	case "join":
		if len(args) != 2 {
			return nil, fmt.Errorf("join() takes two arguments")
		}
		arr, ok := args[1].([]any)
		if !ok {
			return nil, fmt.Errorf("join() expects an array")
		}
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = fmt.Sprint(v)
		}
		return strings.Join(parts, fmt.Sprint(args[0])), nil
	case "split":
		if len(args) != 2 {
			return nil, fmt.Errorf("split() takes two arguments")
		}
		parts := strings.Split(fmt.Sprint(args[1]), fmt.Sprint(args[0]))
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, nil
	case "contains":
		if len(args) != 2 {
			return nil, fmt.Errorf("contains() takes two arguments")
		}
		return containsValue(args[0], args[1]), nil
	case "startsWith":
		if len(args) != 2 {
			return nil, fmt.Errorf("startsWith() takes two arguments")
		}
		return strings.HasPrefix(fmt.Sprint(args[0]), fmt.Sprint(args[1])), nil
	case "endsWith":
		if len(args) != 2 {
			return nil, fmt.Errorf("endsWith() takes two arguments")
		}
		return strings.HasSuffix(fmt.Sprint(args[0]), fmt.Sprint(args[1])), nil
	case "keys":
		if len(args) != 1 {
			return nil, fmt.Errorf("keys() takes one argument")
		}
		m, ok := args[0].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("keys() expects an object")
		}
		out := make([]any, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown function %s()", n.name)
}
func (n *callNode) refs() []Ref {
	out := []Ref{}
	for _, a := range n.args {
		out = append(out, a.refs()...)
	}
	return out
}

type arrayNode struct{ items []node }

func (n *arrayNode) eval(env *Env) (any, error) {
	out := make([]any, len(n.items))
	for i, item := range n.items {
		v, err := item.eval(env)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
func (n *arrayNode) refs() []Ref {
	out := []Ref{}
	for _, item := range n.items {
		out = append(out, item.refs()...)
	}
	return out
}

type objectNode struct {
	keys []string
	vals []node
}

func (n *objectNode) eval(env *Env) (any, error) {
	out := map[string]any{}
	for i, k := range n.keys {
		v, err := n.vals[i].eval(env)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}
func (n *objectNode) refs() []Ref {
	out := []Ref{}
	for _, v := range n.vals {
		out = append(out, v.refs()...)
	}
	return out
}

// ---- lexer / parser ----

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNumber
	tokString
	tokOp
	tokPunct
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

type lexer struct {
	src  string
	pos  int
	toks []token
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	for l.pos < len(src) {
		c := src[l.pos]
		if unicode.IsSpace(rune(c)) {
			l.pos++
			continue
		}
		start := l.pos
		switch {
		case c == '\'' || c == '"':
			quote := c
			l.pos++
			var b strings.Builder
			closed := false
			for l.pos < len(src) {
				ch := src[l.pos]
				if ch == '\\' && l.pos+1 < len(src) {
					next := src[l.pos+1]
					switch next {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
						b.WriteByte(next)
					}
					l.pos += 2
					continue
				}
				if ch == quote {
					l.pos++
					closed = true
					break
				}
				b.WriteByte(ch)
				l.pos++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string at %d", start)
			}
			l.toks = append(l.toks, token{kind: tokString, text: b.String(), pos: start})
		case unicode.IsDigit(rune(c)) || (c == '.' && l.pos+1 < len(src) && unicode.IsDigit(rune(src[l.pos+1]))):
			l.pos++
			for l.pos < len(src) && (unicode.IsDigit(rune(src[l.pos])) || src[l.pos] == '.') {
				l.pos++
			}
			l.toks = append(l.toks, token{kind: tokNumber, text: src[start:l.pos], pos: start})
		case unicode.IsLetter(rune(c)) || c == '_':
			l.pos++
			for l.pos < len(src) && (unicode.IsLetter(rune(src[l.pos])) || unicode.IsDigit(rune(src[l.pos])) || src[l.pos] == '_') {
				l.pos++
			}
			l.toks = append(l.toks, token{kind: tokIdent, text: src[start:l.pos], pos: start})
		default:
			two := ""
			if l.pos+1 < len(src) {
				two = src[l.pos : l.pos+2]
			}
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||":
				l.pos += 2
				l.toks = append(l.toks, token{kind: tokOp, text: two, pos: start})
				continue
			}
			switch c {
			case '<', '>', '+', '-', '*', '/', '%', '!':
				l.pos++
				l.toks = append(l.toks, token{kind: tokOp, text: string(c), pos: start})
			case '(', ')', '[', ']', '{', '}', ',', '.', '?', ':':
				l.pos++
				l.toks = append(l.toks, token{kind: tokPunct, text: string(c), pos: start})
			default:
				return nil, fmt.Errorf("unexpected character %q at %d", c, start)
			}
		}
	}
	l.toks = append(l.toks, token{kind: tokEOF, pos: len(src)})
	return l.toks, nil
}

type parser struct {
	toks []token
	pos  int
}

func ParseExpression(src string) (node, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("expression is empty")
	}
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().text)
	}
	return n, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) accept(text string) bool {
	if p.peek().text == text {
		p.pos++
		return true
	}
	return false
}

func (p *parser) parseTernary() (node, error) {
	cond, err := p.parseBinary(1)
	if err != nil {
		return nil, err
	}
	if p.accept("?") {
		yes, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if !p.accept(":") {
			return nil, fmt.Errorf("expected ':' in ternary expression")
		}
		no, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		return &ternaryNode{cond: cond, yes: yes, no: no}, nil
	}
	return cond, nil
}

func precedence(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=":
		return 3
	case "<", "<=", ">", ">=", "in":
		return 4
	case "+", "-":
		return 5
	case "*", "/", "%":
		return 6
	}
	return 0
}

func (p *parser) parseBinary(min int) (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op := t.text
		if t.kind == tokIdent && t.text == "in" {
			op = "in"
		}
		prec := precedence(op)
		if prec == 0 || prec < min {
			break
		}
		p.next()
		right, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &binaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.accept("!") {
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "!", expr: n}, nil
	}
	if p.accept("-") {
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &unaryNode{op: "-", expr: n}, nil
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() (node, error) {
	n, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.accept("."):
			t := p.next()
			if t.kind != tokIdent {
				return nil, fmt.Errorf("expected field name after '.'")
			}
			n = &memberNode{base: n, name: t.text}
		case p.accept("["):
			idx, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if !p.accept("]") {
				return nil, fmt.Errorf("expected ']'")
			}
			n = &indexNode{base: n, idx: idx}
		case p.accept("("):
			ident, ok := n.(*identNode)
			if !ok {
				return nil, fmt.Errorf("only named functions can be called")
			}
			args := []node{}
			if !p.accept(")") {
				for {
					a, err := p.parseTernary()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.accept(")") {
						break
					}
					if !p.accept(",") {
						return nil, fmt.Errorf("expected ',' or ')'")
					}
				}
			}
			n = &callNode{name: ident.name, args: args}
		default:
			return n, nil
		}
	}
}

func (p *parser) parsePrimary() (node, error) {
	t := p.next()
	switch t.kind {
	case tokNumber:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", t.text)
		}
		return &litNode{value: f}, nil
	case tokString:
		return &litNode{value: t.text}, nil
	case tokIdent:
		switch t.text {
		case "true":
			return &litNode{value: true}, nil
		case "false":
			return &litNode{value: false}, nil
		case "null", "nil":
			return &litNode{value: nil}, nil
		}
		return &identNode{name: t.text}, nil
	case tokPunct:
		switch t.text {
		case "(":
			n, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if !p.accept(")") {
				return nil, fmt.Errorf("expected ')'")
			}
			return n, nil
		case "[":
			items := []node{}
			if !p.accept("]") {
				for {
					item, err := p.parseTernary()
					if err != nil {
						return nil, err
					}
					items = append(items, item)
					if p.accept("]") {
						break
					}
					if !p.accept(",") {
						return nil, fmt.Errorf("expected ',' or ']'")
					}
				}
			}
			return &arrayNode{items: items}, nil
		case "{":
			obj := &objectNode{}
			if !p.accept("}") {
				for {
					keyTok := p.next()
					if keyTok.kind != tokString && keyTok.kind != tokIdent {
						return nil, fmt.Errorf("object keys must be identifiers or strings")
					}
					if !p.accept(":") {
						return nil, fmt.Errorf("expected ':' after object key")
					}
					val, err := p.parseTernary()
					if err != nil {
						return nil, err
					}
					obj.keys = append(obj.keys, keyTok.text)
					obj.vals = append(obj.vals, val)
					if p.accept("}") {
						break
					}
					if !p.accept(",") {
						return nil, fmt.Errorf("expected ',' or '}'")
					}
				}
			}
			return obj, nil
		}
	}
	return nil, fmt.Errorf("unexpected token %q", t.text)
}

// ---- evaluation helpers ----

type CompiledExpr struct {
	AST  node
	Refs []Ref
}

func CompileExpression(src string) (*CompiledExpr, error) {
	ast, err := ParseExpression(src)
	if err != nil {
		return nil, err
	}
	return &CompiledExpr{AST: ast, Refs: ast.refs()}, nil
}

func EvalCompiled(c *CompiledExpr, env *Env) (any, error) {
	return c.AST.eval(env)
}

func EvalExpression(src string, env *Env) (any, error) {
	c, err := CompileExpression(src)
	if err != nil {
		return nil, err
	}
	return EvalCompiled(c, env)
}

// templateParts returns literal and expression segments. exact is true when
// the whole string is one expression, so its type is preserved.
func templateParts(s string) (parts []string, exprs []string, exact bool, err error) {
	var lit strings.Builder
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "${")
		if start < 0 {
			lit.WriteString(s[i:])
			break
		}
		start += i
		lit.WriteString(s[i:start])
		parts = append(parts, lit.String())
		lit.Reset()
		end, eerr := findTemplateEnd(s, start+2)
		if eerr != nil {
			return nil, nil, false, eerr
		}
		expr := s[start+2 : end]
		if strings.TrimSpace(expr) == "" {
			return nil, nil, false, fmt.Errorf("empty interpolation")
		}
		exprs = append(exprs, expr)
		i = end + 1
	}
	if len(exprs) == 0 {
		return []string{s}, nil, false, nil
	}
	parts = append(parts, lit.String())
	exact = len(exprs) == 1 && parts[0] == "" && parts[1] == ""
	return parts, exprs, exact, nil
}

func findTemplateEnd(s string, start int) (int, error) {
	depth := 1
	inQuote := byte(0)
	for i := start; i < len(s); i++ {
		c := s[i]
		if inQuote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inQuote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated interpolation")
}

// CompileTemplates parses every ${...} in a string and returns the compiled
// expressions. Literal strings produce no expressions.
func CompileTemplates(s string) ([]*CompiledExpr, bool, error) {
	parts, exprs, exact, err := templateParts(s)
	_ = parts
	if err != nil {
		return nil, false, err
	}
	out := make([]*CompiledExpr, len(exprs))
	for i, e := range exprs {
		c, err := CompileExpression(e)
		if err != nil {
			return nil, exact, fmt.Errorf("${%s}: %w", e, err)
		}
		out[i] = c
	}
	return out, exact, nil
}

// EvalTemplate evaluates a string that may contain ${...}. When the whole
// string is a single interpolation the original type is preserved.
func EvalTemplate(s string, env *Env) (any, error) {
	parts, exprs, exact, err := templateParts(s)
	if err != nil {
		return nil, err
	}
	if len(exprs) == 0 {
		return s, nil
	}
	if exact {
		return EvalExpression(exprs[0], env)
	}
	var b strings.Builder
	for i, expr := range exprs {
		b.WriteString(parts[i])
		v, err := EvalExpression(expr, env)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case nil:
		case string:
			b.WriteString(x)
		default:
			raw, err := json.Marshal(x)
			if err != nil {
				return nil, err
			}
			b.Write(raw)
		}
	}
	b.WriteString(parts[len(parts)-1])
	return b.String(), nil
}

// EvalValue recursively evaluates templates in maps, slices and strings.
func EvalValue(v any, env *Env) (any, error) {
	switch x := v.(type) {
	case string:
		if strings.Contains(x, "${") {
			return EvalTemplate(x, env)
		}
		return x, nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			ev, err := EvalValue(item, env)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			ev, err := EvalValue(item, env)
			if err != nil {
				return nil, err
			}
			out[k] = ev
		}
		return out, nil
	}
	return v, nil
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func containsValue(container, needle any) bool {
	switch c := container.(type) {
	case []any:
		for _, v := range c {
			if equalJSON(v, needle) {
				return true
			}
		}
	case map[string]any:
		if _, ok := c[fmt.Sprint(needle)]; ok {
			return true
		}
	case string:
		return strings.Contains(c, fmt.Sprint(needle))
	}
	return false
}

func compare(op string, a, b any) (bool, error) {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			switch op {
			case "<":
				return af < bf, nil
			case "<=":
				return af <= bf, nil
			case ">":
				return af > bf, nil
			case ">=":
				return af >= bf, nil
			}
		}
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		switch op {
		case "<":
			return as < bs, nil
		case "<=":
			return as <= bs, nil
		case ">":
			return as > bs, nil
		case ">=":
			return as >= bs, nil
		}
	}
	return false, fmt.Errorf("cannot compare %T and %T", a, b)
}
