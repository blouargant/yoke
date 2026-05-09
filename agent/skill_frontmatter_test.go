package agent

import "testing"

func TestParseSkillFrontMatter(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantName string
		wantDesc string
	}{
		{
			name:     "well-formed",
			in:       "---\nname: foo\ndescription: hello\n---\nbody\n",
			wantName: "foo",
			wantDesc: "hello",
		},
		{
			name:     "quoted value containing colon",
			in:       "---\nname: foo\ndescription: \"a: b\"\n---\nbody\n",
			wantName: "foo",
			wantDesc: "a: b",
		},
		{
			name:     "missing description",
			in:       "---\nname: foo\n---\nbody\n",
			wantName: "foo",
			wantDesc: "",
		},
		{
			name:     "no front matter",
			in:       "just markdown\n",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "malformed yaml does not panic",
			in:       "---\nname: \"unterminated\ndescription: oops\n---\n",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "missing closing delimiter",
			in:       "---\nname: foo\ndescription: bar\n",
			wantName: "",
			wantDesc: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotDesc := parseSkillFrontMatter([]byte(tc.in))
			if gotName != tc.wantName || gotDesc != tc.wantDesc {
				t.Errorf("got (%q, %q), want (%q, %q)", gotName, gotDesc, tc.wantName, tc.wantDesc)
			}
		})
	}
}
