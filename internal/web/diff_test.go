package web

import (
	"reflect"
	"testing"
)

func TestDiffLines(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want []DiffLine
	}{
		{
			name: "identical",
			a:    "foo\nbar\n",
			b:    "foo\nbar\n",
			want: []DiffLine{{Type: "eq", Text: "foo"}, {Type: "eq", Text: "bar"}},
		},
		{
			name: "pure addition",
			a:    "foo\n",
			b:    "foo\nbar\n",
			want: []DiffLine{{Type: "eq", Text: "foo"}, {Type: "add", Text: "bar"}},
		},
		{
			name: "pure deletion",
			a:    "foo\nbar\n",
			b:    "foo\n",
			want: []DiffLine{{Type: "eq", Text: "foo"}, {Type: "del", Text: "bar"}},
		},
		{
			name: "single line changed",
			a:    "a\nfoo: bar\nc\n",
			b:    "a\nfoo: CHANGED\nc\n",
			want: []DiffLine{
				{Type: "eq", Text: "a"},
				{Type: "del", Text: "foo: bar"},
				{Type: "add", Text: "foo: CHANGED"},
				{Type: "eq", Text: "c"},
			},
		},
		{
			name: "both empty",
			a:    "",
			b:    "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffLines(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DiffLines(%q, %q) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
