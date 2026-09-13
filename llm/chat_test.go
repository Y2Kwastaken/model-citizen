package llm

import "testing"

func TestCleanReply(t *testing.T) {
	history := []Message{
		{Who: User, Name: "miles_dev (Miles)", Content: "whats up"},
		{Who: Self, Name: "modelcitizen", Content: "nothing"},
	}
	cases := []struct{ in, want string }{
		{"lmao no", "lmao no"},
		{"lmao no\n\nmiles_dev (Miles): but why tho\nmiles_dev (Miles): come on", "lmao no"},
		{"miles_dev (Miles): ok here we go\nit's a sandwich", "ok here we go\nit's a sandwich"},
		{"[3:52 PM] miles_dev: lmao\n[3:52 PM] modelcitizen: depends", ""},
		{"note: don't do that\nseriously", "note: don't do that\nseriously"},
		{"modelcitizen: rip dog man", "rip dog man"},
		{"  padded  \n", "padded"},
	}
	for _, c := range cases {
		if got := cleanReply(c.in, history); got != c.want {
			t.Errorf("cleanReply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
