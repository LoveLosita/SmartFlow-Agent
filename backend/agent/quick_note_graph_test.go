package agent

import "testing"

func TestDeriveQuickNoteTitleFromInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "保留核心事项并去掉尾部提醒口头语",
			input: "明天上午12点我要去取快递，到时候记得q我",
			want:  "明天上午12点我要去取快递",
		},
		{
			name:  "去掉常见前缀口头语",
			input: "提醒我周五下午三点交实验报告",
			want:  "周五下午三点交实验报告",
		},
		{
			name:  "空输入兜底",
			input: "   ",
			want:  "这条任务",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveQuickNoteTitleFromInput(tc.input)
			if got != tc.want {
				t.Fatalf("title 提取不符合预期，got=%q want=%q", got, tc.want)
			}
		})
	}
}
