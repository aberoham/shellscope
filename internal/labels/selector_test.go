package labels

import "testing"

func TestParseSelector(t *testing.T) {
	cases := []struct {
		in       string
		want     int
		wantErr  bool
		firstKey string
	}{
		{in: "", want: 0},
		{in: "operator.type=human", want: 1, firstKey: "operator.type"},
		{in: "operator.type=agent,agent.tool=claude-code", want: 2, firstKey: "operator.type"},
		{in: " a = b , c = d ", want: 2, firstKey: "a"},
		{in: "trailing,", want: 0, wantErr: true},
		{in: "noequals", wantErr: true},
		{in: "=novalue", wantErr: true},
		{in: "k=", wantErr: true},
		{in: "k=v,k=w", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseSelector(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("%q: err=%v want_err=%v", tc.in, err, tc.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if len(got) != tc.want {
			t.Errorf("%q: got %d reqs, want %d", tc.in, len(got), tc.want)
		}
		if tc.firstKey != "" && got[0].Key != tc.firstKey {
			t.Errorf("%q: first key %q want %q", tc.in, got[0].Key, tc.firstKey)
		}
	}
}
