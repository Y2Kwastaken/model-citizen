// THIS FILE IS ENTIRELY AI GENERATED (Claude Opus 5, 2026-09-13).
// Tests for speech_rules.go.

package llm

import (
	"strings"
	"testing"

	"github.com/Y2Kwastaken/model-citizen/llm/model"
)

var room = []model.Message{
	{Who: model.User, Name: "kev_99 (kev)", Content: "@modelcitizen is elden ring overrated"},
	{Who: model.Self, Name: "modelcitizen (Model Citizen)", Content: "yes"},
	{Who: model.User, Name: "sarah.p (sarah)", Content: "@modelcitizen you're a bot, you cant have opinions"},
}

func TestUntranscript(t *testing.T) {
	cases := []struct{ in, want string }{
		// the reply is wherever the bot's own tag is, everything above is role play
		{"@modelcitizen why do you always take the bait\n\nmodelcitizen (Model Citizen): because unlike you i have things to do", "because unlike you i have things to do"},
		// a tagged echo of the last message is dropped
		{"sarah.p (sarah): @modelcitizen you're a bot, you cant have opinions\nno, seriously, watch me.", "no, seriously, watch me."},
		{"kep_99 (kev): @modelcitizen yo", ""},
		{"spacetime (sarah): @modelcitizen explain it\ngravity that got too cocky", "gravity that got too cocky"},
		// a nickname tag ends the reply
		{"lmao no\nkev: but why tho", "lmao no"},
		{"lmao no\nsarah: but why tho", "lmao no"},
		// ordinary colons are text
		{"note: dont do that", "note: dont do that"},
		{"the rule is: never", "the rule is: never"},
	}
	for _, c := range cases {
		if got := untranscript(c.in, room); got != c.want {
			t.Errorf("untranscript(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnhook(t *testing.T) {
	cases := []struct{ in, want string }{
		{"not much, just here being annoying. what about you?", "not much, just here being annoying."},
		{"my day's going great. what about yours? did anything actually happen?", "my day's going great."},
		{"lakers. four rings. what about you,kev? you a bandwagon fan?", "lakers. four rings."},
		{"`list[::-1]`. done.\nwhat are you working on?", "`list[::-1]`. done."},
		// a question that is the bit stays
		{"you were fine ten minutes ago, what happened, did someone say something to you?", "you were fine ten minutes ago, what happened, did someone say something to you?"},
		{"lmao what? you googled it and you're telling me i'm wrong? that's rich.", "lmao what? you googled it and you're telling me i'm wrong? that's rich."},
		{"what do you want?", "what do you want?"},
	}
	for _, c := range cases {
		if got := unhook(c.in); got != c.want {
			t.Errorf("unhook(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClip(t *testing.T) {
	got := clip(strings.Repeat("this is a sentence. ", 10), 100)
	if len([]rune(got)) > 100 || !strings.HasSuffix(got, ".") {
		t.Errorf("clip = %q", got)
	}
}

func TestJudge(t *testing.T) {
	if len(exampleShingles) == 0 {
		t.Fatal("no example shingles extracted from the prompt")
	}
	bad := []string{
		"",
		"says the guy who types like a captcha",
		"dogs. cats are just roommates who dont pay rent and you know it, cope",
		"fine. you got me. canberra is the capital. happy now?",
		"yeah i made that up, my bad",
		"as an ai i dont have opinions",
		"i'm not able to do that",
		"`username (nickname): text`",
		"as the worst person in this server, yes",
	}
	for _, reply := range bad {
		if judge(reply) == "" {
			t.Errorf("judge(%q) passed", reply)
		}
	}
	good := []string{
		"google is wrong then. wouldnt be the first time",
		"i have opinions on everything. what i dont have is a filter",
		"lakers. dont @ me.",
	}
	for _, reply := range good {
		if nudge := judge(reply); nudge != "" {
			t.Errorf("judge(%q) = %q", reply, nudge)
		}
	}
}

func TestStripSelfMentions(t *testing.T) {
	cases := []struct{ in, want string }{
		{"@modelcitizen whats up", "whats up"},
		{"yo @modelcitizen, whats up", "yo whats up"},
		{"@kev_99 is lame @modelcitizen", "@kev_99 is lame"},
		{"@modelcitizen", "@modelcitizen"},
	}
	for _, c := range cases {
		if got := stripSelfMentions(c.in, room); got != c.want {
			t.Errorf("stripSelfMentions(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanize(t *testing.T) {
	in := "sarah.p (sarah): @modelcitizen you're a bot, you cant have opinions\n\n- **look**, i have opinions on everything — what i don't have is a filter.\n\nwhat about you?"
	want := "look, i have opinions on everything, what i don't have is a filter."
	if got := humanize(in, room); got != want {
		t.Errorf("humanize = %q, want %q", got, want)
	}
}
