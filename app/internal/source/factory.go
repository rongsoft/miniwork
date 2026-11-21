package source

import "strings"

// Spec 是选择数据源所需的全部信息。放在这里而不是让人各自 switch，
// 是为了保证 CLI 和桌面端选中同一个实现。
type Spec struct {
	Kind      string // mock | remote
	BaseURL   string // remote 专用
	Token     string // remote 专用
	MockSeed  uint64
	MockCount int
	MockNoise float64
}

// Build 按配置挑实现，并返回一句「为什么是它」，用于日志和界面角标。
//
// 唯一的降级规则：配了 remote 却没填地址，退回 mock。
// 这必须是显式的降级——界面上要知道自己在看示例数据，不能让人以为那是真榜单。
func Build(sp Spec) (Source, string) {
	if strings.ToLower(strings.TrimSpace(sp.Kind)) == "remote" {
		if strings.TrimSpace(sp.BaseURL) != "" {
			return NewRemote(sp.BaseURL, sp.Token), "线上数据"
		}
		return newMock(sp), "示例数据（未配置服务端地址）"
	}
	return newMock(sp), "示例数据"
}

func newMock(sp Spec) *Mock {
	m := &Mock{Seed: sp.MockSeed, Count: sp.MockCount, Noise: sp.MockNoise}
	if m.Seed == 0 {
		m.Seed = 1
	}
	if m.Count <= 0 {
		m.Count = 40
	}
	if m.Noise <= 0 {
		m.Noise = 0.3
	}
	return m
}
