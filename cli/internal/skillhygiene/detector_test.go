package skillhygiene

import (
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "unsafe bare variable in plain text",
			content: "for x in $LIST;\n",
			want:    1,
		},
		{
			name:    "array expansion is safe",
			content: "for x in \"${arr[@]}\";\n",
			want:    0,
		},
		{
			name:    "command substitution is safe",
			content: "for x in $(seq 1 5);\n",
			want:    0,
		},
		{
			name:    "literal list is safe",
			content: "for x in a b c;\n",
			want:    0,
		},
		{
			name:    "bare variable inside text fence skipped",
			content: "```text\nfor x in $LIST\n```\n",
			want:    0,
		},
		{
			name:    "bare variable inside bash fence caught",
			content: "```bash\nfor x in $LIST;\n```\n",
			want:    1,
		},
		{
			name:    "two unsafe lines give two matches",
			content: "for x in $LIST;\nfor y in $ITEMS;\n",
			want:    2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.content)
			if len(got) != tc.want {
				t.Errorf("Detect(%q) = %d matches, want %d\nmatches: %+v",
					tc.content, len(got), tc.want, got)
			}
		})
	}
}
