package workflow

import "testing"

func testEnv() *Env {
	return &Env{
		Inputs: map[string]any{"folder": "docs", "n": float64(3)},
		Steps: map[string]StepView{
			"list": {Status: "succeeded", Output: map[string]any{"files": []any{
				map[string]any{"path": "a.md"},
				map[string]any{"path": "b.md"},
			}, "count": float64(2)}},
		},
		Run: map[string]any{"id": "run_1", "source": "manual"},
	}
}

func TestInterpolationPreservesType(t *testing.T) {
	v, err := EvalTemplate("${steps.list.output.count}", testEnv())
	if err != nil {
		t.Fatal(err)
	}
	if v != float64(2) {
		t.Fatalf("expected number, got %#v", v)
	}
	v, err = EvalTemplate("${inputs.folder}/x", testEnv())
	if err != nil {
		t.Fatal(err)
	}
	if v != "docs/x" {
		t.Fatalf("got %#v", v)
	}
}

func TestExpressions(t *testing.T) {
	cases := []struct {
		expr string
		want any
	}{
		{`size(steps.list.output.files) == 2`, true},
		{`steps.list.output.count > 1 && inputs.folder == "docs"`, true},
		{`steps.list.output.files[1].path`, "b.md"},
		{`"hello " + inputs.folder`, "hello docs"},
		{`inputs.n * 2 + 1`, float64(7)},
	}
	for _, c := range cases {
		got, err := EvalExpression(c.expr, testEnv())
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

func TestExpressionReferences(t *testing.T) {
	c, err := CompileExpression(`steps.list.output.count > 0 && inputs.folder != ""`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Refs) < 2 {
		t.Fatalf("expected refs, got %+v", c.Refs)
	}
}

func TestItemAlias(t *testing.T) {
	env := testEnv()
	env.HasItem = true
	env.Item = map[string]any{"path": "x"}
	env.Vars = map[string]any{"file": map[string]any{"path": "x"}}
	v, err := EvalTemplate("${file.path}", env)
	if err != nil || v != "x" {
		t.Fatalf("alias eval: %#v %v", v, err)
	}
}
