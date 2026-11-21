// Package flow 是工作流引擎。
//
// 三件事严格分开：
//   - Step：无状态、幂等、可重试
//   - Flow：Step 的有序组合，可存库、可由用户修改
//   - Job：Flow 的一次执行实例（在 store 里落地）
//
// Step 之间传 map[string]any 而不是 struct，是刻意的：Flow 要能序列化进 SQLite
// 并允许用户改，用 struct 就把「改工作流不发版」这条路堵死了。
package flow

import (
	"context"
	"fmt"
)

// Emit 汇报进度。pct 取 0-100。
type Emit func(pct int, msg string)

// Step 是一个原子动作。
type Step interface {
	Name() string
	Run(ctx context.Context, in map[string]any, emit Emit) (map[string]any, error)
}

// StepRef 是 Flow 里对 Step 的一次引用，Params 用于同一 Step 的不同配置。
type StepRef struct {
	Step   string         `json:"step"`
	Params map[string]any `json:"params,omitempty"`
}

// Flow 是一条可执行的流水线。
type Flow struct {
	Key   string    `json:"key"`
	Name  string    `json:"name"`
	Steps []StepRef `json:"steps"`
}

// Engine 持有 Step 注册表。
type Engine struct {
	steps map[string]Step
}

func NewEngine() *Engine {
	return &Engine{steps: make(map[string]Step)}
}

// Register 注册一个 Step。重名直接报错，避免静默覆盖。
func (e *Engine) Register(s Step) error {
	name := s.Name()
	if name == "" {
		return fmt.Errorf("step 名为空")
	}
	if _, ok := e.steps[name]; ok {
		return fmt.Errorf("step 重复注册: %s", name)
	}
	e.steps[name] = s
	return nil
}

// Steps 返回已注册的 Step 名，用于诊断。
func (e *Engine) Steps() []string {
	out := make([]string, 0, len(e.steps))
	for k := range e.steps {
		out = append(out, k)
	}
	return out
}

// Run 顺序执行 Flow。每个 Step 的输出合并进同一个 map，后者可覆盖前者。
func (e *Engine) Run(ctx context.Context, f Flow, in map[string]any, emit Emit) (map[string]any, error) {
	if emit == nil {
		emit = func(int, string) {}
	}
	if len(f.Steps) == 0 {
		return nil, fmt.Errorf("flow %s 没有任何 step", f.Key)
	}

	state := make(map[string]any, len(in)+len(f.Steps)*2)
	for k, v := range in {
		state[k] = v
	}

	total := len(f.Steps)
	for i, ref := range f.Steps {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		s, ok := e.steps[ref.Step]
		if !ok {
			return state, fmt.Errorf("未知 step: %s", ref.Step)
		}
		state["_step"] = ref.Step
		state["_params"] = ref.Params

		base := i * 100 / total
		span := 100 / total
		stepEmit := func(pct int, msg string) {
			emit(base+pct*span/100, msg)
		}
		emit(base, "开始 "+ref.Step)

		out, err := s.Run(ctx, state, stepEmit)
		if err != nil {
			return state, fmt.Errorf("step %s 失败: %w", ref.Step, err)
		}
		for k, v := range out {
			state[k] = v
		}
	}
	emit(100, "完成")
	return state, nil
}
