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
		{"thought\n<channel|>fine, i wrote it down.", "fine, i wrote it down."},
		{"<|channel>thought\nhm\n<channel|>lmao no", "lmao no"},
		{"[12:04:31] nah", "nah"},
		{"noted, quincy's a bitch\n[19:54:44]Memory [miles_dev]: TomTheBomb (Quincy) is a bitch", "noted, quincy's a bitch"},
		{"thats a mailing address with commitment issues. [19:55:16]Memory [miles_dev]: Noah is Noah Aney", "thats a mailing address with commitment issues."},
		{"Memory [miles_dev]: he said it first", ""},
	}
	for _, c := range cases {
		if got := cleanReply(c.in, history, 650); got != c.want {
			t.Errorf("cleanReply(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
