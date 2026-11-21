package flow

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeStep struct {
	name string
	out  map[string]any
	err  error
}

func (f *fakeStep) Name() string { return f.name }

func (f *fakeStep) Run(_ context.Context, in map[string]any, emit Emit) (map[string]any, error) {
	emit(50, "half")
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	e := NewEngine()
	if err := e.Register(&fakeStep{name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Register(&fakeStep{name: "a"}); err == nil {
		t.Fatal("重复注册必须报错，否则会静默覆盖")
	}
}

func TestRunMergesOutputsAcrossSteps(t *testing.T) {
	e := NewEngine()
	e.Register(&fakeStep{name: "a", out: map[string]any{"x": 1}})
	e.Register(&fakeStep{name: "b", out: map[string]any{"y": 2}})

	var last int
	got, err := e.Run(context.Background(),
		Flow{Key: "f", Steps: []StepRef{{Step: "a"}, {Step: "b"}}},
		map[string]any{"in": "v"},
		func(pct int, _ string) { last = pct })
	if err != nil {
		t.Fatal(err)
	}
	if got["x"] != 1 || got["y"] != 2 || got["in"] != "v" {
		t.Fatalf("输出应合并上游与各 step 结果，实际 %+v", got)
	}
	if last != 100 {
		t.Fatalf("结束时进度应为 100，实际 %d", last)
	}
}

func TestRunWrapsStepErrorWithName(t *testing.T) {
	e := NewEngine()
	e.Register(&fakeStep{name: "boom", err: errors.New("内部错误")})

	_, err := e.Run(context.Background(), Flow{Key: "f", Steps: []StepRef{{Step: "boom"}}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误必须带 step 名，便于定位，实际 %v", err)
	}
}

func TestRunRejectsUnknownStep(t *testing.T) {
	e := NewEngine()
	_, err := e.Run(context.Background(), Flow{Key: "f", Steps: []StepRef{{Step: "ghost"}}}, nil, nil)
	if err == nil {
		t.Fatal("未知 step 必须报错")
	}
}

func TestRunRespectsContextCancel(t *testing.T) {
	e := NewEngine()
	e.Register(&fakeStep{name: "a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Run(ctx, Flow{Key: "f", Steps: []StepRef{{Step: "a"}}}, nil, nil); err == nil {
		t.Fatal("已取消的 context 应立即返回错误")
	}
}
