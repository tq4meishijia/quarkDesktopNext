// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

package app

import "testing"

// TestParseCookieNames 凭证条目统计：设置页要告诉用户「存了几项 Cookie」，
// 统计必须忽略 $Version/$Path 之类附加属性与空段，否则条数会虚高。
func TestParseCookieNames(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"__pus=1", 1},
		{"__pus=1; __puus=2", 2},
		{"a=1;b=2;c=3", 3},
		{"a=1;;b=2", 2},        // 空段
		{"$Version=1; a=1", 1}, // 附加属性
		{"noequals; a=1", 1},   // 缺 = 的脏数据
		{"=v; a=1", 1},         // 缺名
	}
	for _, c := range cases {
		if got := len(parseCookieNames(c.in)); got != c.want {
			t.Errorf("parseCookieNames(%q) 条目数 = %d，期望 %d", c.in, got, c.want)
		}
	}
}
