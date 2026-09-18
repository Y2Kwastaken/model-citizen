package llm

import (
	"testing"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

func TestCleanReply(t *testing.T) {
	history := []model.Message{
		{Who: model.User, Name: "miles_dev (Miles)", Content: "whats up"},
		{Who: model.Self, Name: "modelcitizen", Content: "nothing"},
	}
	cases := []struct{ in, want string }{
		{"lmao no", "lmao no"},
		{"lmao no\n\nmiles_dev (Miles): but why tho\nmiles_dev (Miles): come on", "lmao no"},
		// a multi-line reply is the model's call, only the echoed tag goes
		{"miles_dev (Miles): ok here we go\nit's a sandwich", "ok here we go\nit's a sandwich"},
		{"note: don't do that\nseriously", "note: don't do that\nseriously"},
		{"modelcitizen: rip dog man", "rip dog man"},
		{"  padded  \n", "padded"},
		{"`modelcitizen: not much, you`</think>normies trying to figure it out", "normies trying to figure it out"},
		{"<think>\nlet me think\n</think>\nlmao no", "lmao no"},
		{"<think>lmao no", "lmao no"},
		{"[12:04:31] nah", "nah"},
	}
	for _, c := range cases {
		if got := cleanReply(c.in, history); got != c.want {
			t.Errorf("cleanReply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
