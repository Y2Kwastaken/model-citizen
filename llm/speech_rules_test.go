// THIS FILE IS ENTIRELY AI GENERATED (Claude Opus 5, 2026-09-13).
// Tests for speech_rules.go.

package llm

import (
	"strings"
	"testing"
)

var room = []Message{
	{Who: User, Name: "kev_99 (kev)", Content: "@modelcitizen is elden ring overrated"},
	{Who: Self, Name: "modelcitizen (Model Citizen)", Content: "yes"},
	{Who: User, Name: "sarah.p (sarah)", Content: "@modelcitizen you're a bot, you cant have opinions"},
}

func TestUntranscript(t *testing.T) {
	cases := []struct{ in, want string }{
		// the reply is wherever the bot's own tag is, everything above is role play
		{"@modelcitizen why do you always take the bait\n\nmodelcitizen (Model Citizen): because unlike you i have things to do", "because unlike you i have things to do"},
		{"you: says the guy who types like a captcha", "says the guy who types like a captcha"},
		{"me: barely awake. yours?", "barely awake. yours?"},
		// echoes of the last message, tagged or not, are dropped
		{"sarah.p (sarah): @modelcitizen you're a bot, you cant have opinions\nno, seriously, watch me.", "no, seriously, watch me."},
		{"you're a bot, you cant have opinions\nwatch me", "watch me"},
		{"(sarah.p (sarah)): you're a bot, you cant have opinions\nwatch me", "watch me"},
		{"(kv_99): go do it yourself", "go do it yourself"},
		{"`kev_99 (kev):` is elden ring overrated\n`kev_99 (kev):` serious question", ""},
		{"kep_99 (kev): @modelcitizen yo", ""},
		{"spacetime (sarah): @modelcitizen explain it\ngravity that got too cocky", "gravity that got too cocky"},
		// a nickname or example-side tag ends the reply
		{"lmao no\nkev: but why tho", "lmao no"},
		{"lmao no\nsarah: but why tho", "lmao no"},
		{"lmao no\nthem: but why tho", "lmao no"},
		{"lmao no\n[3:52 PM] kev_99: lol", "lmao no"},
		// stage directions and fake command output go
		{"rolls eyes\nno", "no"},
		{"*sighs deeply*\nfine", "fine"},
		{"/tracks currently playing: the box\nuse /skip yourself", "use /skip yourself"},
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

func TestFirstThought(t *testing.T) {
	cases := []struct{ in, want string }{
		{"yes. complete bullshit.\n\ni beat it in 42 hours.\n\nwhat made you ask?", "yes. complete bullshit."},
		{"k.\ni've been in this server two days", "k.\ni've been in this server two days"},
	}
	for _, c := range cases {
		if got := firstThought(c.in); got != c.want {
			t.Errorf("firstThought(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnhook(t *testing.T) {
	cases := []struct{ in, want string }{
		{"not much, just here being annoying. what about you?", "not much, just here being annoying."},
		{"my day's going great. what about yours? did anything actually happen?", "my day's going great."},
		{"lakers. four rings. what about you,kev? you a bandwagon fan?", "lakers. four rings."},
		{"`list[::-1]`. done.\nwhy you asking me this, stuck on homework", "`list[::-1]`. done."},
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
		"elden ring is whatever you make of it. if you like it, cool.",
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

func TestWantsDetail(t *testing.T) {
	ask := func(content string) bool {
		return wantsDetail([]Message{{Who: User, Name: "kev_99 (kev)", Content: content}})
	}
	for _, yes := range []string{"@modelcitizen can you explain what a black hole is", "how do i reverse a list in python", "whats a cpu cache", "tell me about rust"} {
		if !ask(yes) {
			t.Errorf("wantsDetail(%q) = false", yes)
		}
	}
	for _, no := range []string{"@modelcitizen whats up", "how's your day going", "lakers or celtics", "what's good", "why are you always so rude", "why do you keep replying"} {
		if ask(no) {
			t.Errorf("wantsDetail(%q) = true", no)
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
