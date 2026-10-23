package spider

import "testing"

// TestStripWorkPrefix 验证作品号前缀剥离：asmr.one 的 tracks 接口只接受纯数字 id。
func TestStripWorkPrefix(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"RJ 全码带前导零", "RJ01705068", "01705068"},
		{"RJ 短码", "RJ123456", "123456"},
		{"小写前缀", "rj123456", "123456"},
		{"VJ 作品", "VJ015456", "015456"},
		{"纯数字原样透传", "1705068", "1705068"},
		{"首尾空白", " RJ123456 ", "123456"},
		{"仅前缀", "RJ", ""},
		{"空串", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripWorkPrefix(tc.input); got != tc.want {
				t.Fatalf("stripWorkPrefix(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
