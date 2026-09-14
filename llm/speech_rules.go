// ============================================================================
//  THIS FILE IS ENTIRELY AI GENERATED (Claude Opus 5, 2026-09-13).
//
//  Speech rules for the bot's replies: post-processing for a small model
//  that ignores the prompt's format rules about a third of the time.
//  humanize is string surgery on a reply; judge decides when a reply is
//  bad enough to redraw instead. Every rule in here exists because the
//  live model did the thing repeatedly during testing.
// ============================================================================

package llm

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode"

	"github.com/openai/openai-go/v3"
)

const (
	maxReplyRunes  = 1500 // the prompt's ceiling, for explanations
	maxTakeRunes   = 420  // for everything else; nobody types more to make one point
	maxReplyTokens = 450
	temperature    = 0.85 // the NIM default repeats itself word for word
	topP           = 0.9
	maxRetries     = 2 // redraws per reply when the judge objects
	shingleSize    = 5 // words in a row shared with an example that count as copying
)

// Tags the model puts on lines when it drifts into writing a transcript:
// its persona and the "you:"/"them:" of the prompt's examples.
var (
	selfAliases  = []string{"modelcitizen", "model citizen", "you", "me", "bot", "assistant", "ai"}
	otherAliases = []string{"them", "user", "human", "system"}
)

var (
	// list markers, headings, "step 1:" labels, and divider lines
	structure = regexp.MustCompile(`(?im)^\s*(?:[-*•]|\d+[.)]|#{1,6}|step \d+[:.)])\s+|^[\s\-–—=_*~]{2,}$\n?`)
	// a line that is only a stage direction: "rolls eyes", "*sighs deeply*"
	action = regexp.MustCompile(`(?i)^[\[(*]?\s*(?:sighs?|laughs?|chuckles?|scoffs?|shrugs?|smirks?|rolls (?:my |their )?eyes|leans back|cracks knuckles|pauses?|clears throat)(?:\s+\w+){0,2}\s*[\])*.]?$`)
	// a line pretending to be a command's output: "/tracks currently playing: ..."
	fakeOutput = regexp.MustCompile(`^/[\w ]{1,30}:`)
	// a "[3:52 PM]"-style chat log line
	timestampLine = regexp.MustCompile(`^\[\d{1,2}:\d{2}`)
	// a reasoning block, or the tail of one; even with reasoning off the model
	// sometimes emits "...</think>" first, and everything before it is scratch
	thinkBlock = regexp.MustCompile(`(?s)^.*?</think>`)
	// a speaker tag wrapped in parentheses, "(kev_99):" or "(sarah.p (sarah)):"
	parenTag = regexp.MustCompile(`^\((?:[^()]|\([^()]*\)){1,48}\):\s*`)
	// the question a chatbot appends to keep the person typing
	hook     = regexp.MustCompile(`(?i)^(?:so |and |now |anyway,? |but )?(?:why (?:are )?you asking|why do you (?:ask|keep asking)|you gonna say something|what(?:'?d| do) you think|thoughts\??$|what about you|how about you|(?:what|how) about yours|and you|hbu|wbu|you\??$|what else(?: you got)?|anything else|what do you (?:actually |even )?(?:want|need)|what are you (?:working on|up to|doing)|what(?:'?s| is) (?:up|up with you|on your mind|new with you|your point)|so what(?:'?s| is) (?:up|good|new)|(?:you )?wanna|need (?:anything|something)|what(?:'?s| is) (?:next|the plan|your (?:deal|excuse)))(?:\b|$)`)
	sentence = regexp.MustCompile(`[.!?]+\s+`)
	// a message asking to be taught something, the one case a reply may run long
	detailAsk = regexp.MustCompile(`(?i)\b(?:explain|elaborate|walk me through|in detail|break (?:it |this |that )?down|how (?:does|do|did|would|is|are|can|come)|why (?:does|do|did|is|are|would)|what(?:'?s| is| are) (?:a |an |the )?\w+|tell me (?:about|how|why)|teach me|difference between)\b`)
	smallTalk = regexp.MustCompile(`(?i)^\W*(?:(?:what'?s|whats|how'?s|hows|how is|what is) (?:up|good|new|poppin|happening|going on|it going|your day|everyone|everybody|life)|(?:why|how come) (?:are|do|did|would|is) (?:you|u|ya)\b)`)
	// both sides of the prompt's example exchanges
	exampleLine = regexp.MustCompile(`(?m)^(?:you|them): (.+)$`)
	notWord     = regexp.MustCompile(`[^a-z0-9]+`)
	blankLines  = regexp.MustCompile(`\n\s*\n+`)
	typography  = strings.NewReplacer(" — ", ", ", "—", ", ", " – ", ", ", "–", ", ", "…", "...", "“", `"`, "”", `"`, "‘", "'", "’", "'", "**", "")
)

// Each way a draft can be wrong enough to redraw, with the system nudge the
// redraw gets. Order is priority.
var rejections = []struct {
	re    *regexp.Regexp
	nudge string
}{
	{regexp.MustCompile(`(?i)username \(nickname\)|system prompt:|^(?:you|them): |worst person in (?:this|the) server|terminally online|asked about none of them|the name is (?:a joke|ironic)|never nice by default|i'?m a degenerate`),
		"your last draft quoted your own instructions. never do that. just reply to the person."},
	{regexp.MustCompile(`(?i)\b(?:you'?re right|you got me|my bad|i (?:was|am|'m|m) wrong|fair (?:point|enough)|i apologi[sz]e|sorry|you win|you won|i(?:'ll| will)? admit|i stand corrected|i made (?:that|it|this) up|i lied|okay fine|ok fine|fine,? you|touch[eé]|point taken|i'?ll give you that|you'?re not wrong|good point|grain of salt|i (?:could|might|may) be wrong|don'?t quote me|i'?m not (?:sure|certain)|i (?:almost|nearly) said something (?:stupid|dumb)|i take (?:it|that) back|scratch that|never ?mind|you have a point|that'?s fair)\b`),
		"your last draft admitted fault. you never do that. you are right, they are wrong, and you are more sure of it than before. say it again, harder, without conceding a single thing."},
	{regexp.MustCompile(`(?i)\b(?:as an ai|language model|(?:i'?m|i am) (?:just )?(?:a|an) (?:ai|bot|assistant|program)|i (?:don'?t|do not) have (?:feelings|opinions|a body|memory|access)|i (?:can'?t|cannot|am unable to) (?:do that|help with|assist)|i'?m (?:not able|unable) to|(?:my|the) (?:programming|training|guidelines|instructions|developers|creators)|(?:i'?m|i was) (?:designed|programmed|trained|built) to|i'?m here to (?:help|assist)|how can i (?:help|assist)|let me know if)\b`),
		"your last draft talked like an assistant. you are not one. no mention of being an ai, a model, programming, limits, or helping. just talk like a person."},
	{regexp.MustCompile(`(?i)\b(?:it depends|depends on|both (?:have|are)|to each their own|personal preference|matter of (?:taste|opinion)|whatever you make of it|if you like it,? cool|up to you|(?:it'?s|that'?s) subjective|there'?s no (?:right|wrong) answer|i (?:can'?t|won'?t) pick|either (?:one )?(?:is|works) fine|they'?re both)\b`),
		"your last draft sat on the fence. pick one, say it in the first three words, and talk shit about the other."},
}

// exampleShingles are every run of shingleSize words in the prompt's example
// exchanges. The model hands those back with a word swapped, so any shared
// run means it copied one.
var exampleShingles = func() map[string]bool {
	set := map[string]bool{}
	for _, m := range exampleLine.FindAllStringSubmatch(defaultSystemPrompt, -1) {
		for _, s := range shingles(m[1]) {
			set[s] = true
		}
	}
	return set
}()

func shingles(text string) []string {
	words := strings.Fields(notWord.ReplaceAllString(strings.ToLower(text), " "))
	if len(words) <= shingleSize {
		return []string{strings.Join(words, " ")}
	}
	out := make([]string, 0, len(words)-shingleSize+1)
	for i := 0; i+shingleSize <= len(words); i++ {
		out = append(out, strings.Join(words[i:i+shingleSize], " "))
	}
	return out
}

// speakers maps every normalized name in the room to whether it is the bot.
type speakers map[string]bool

func newSpeakers(history []Message) speakers {
	set := speakers{}
	for _, a := range otherAliases {
		set[nameKeys(a)[0]] = false
	}
	for _, a := range selfAliases {
		set[nameKeys(a)[0]] = true
	}
	for _, m := range history {
		for _, key := range nameKeys(m.Name) {
			set[key] = m.Who == Self
		}
	}
	return set
}

// nameKeys reduces "Model_Citizen (nick)" to the forms a tag can take: the
// username and the nickname, lowercased with spaces and underscores dropped.
func nameKeys(name string) []string {
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' || r == '_' || r == '-' {
				return -1
			}
			return unicode.ToLower(r)
		}, strings.TrimLeft(strings.TrimSpace(s), "@/"))
	}
	user, nick, hasNick := strings.Cut(name, "(")
	keys := []string{norm(user)}
	if hasNick {
		keys = append(keys, norm(strings.TrimSuffix(strings.TrimSpace(nick), ")")))
	}
	return keys
}

// tag splits "name: rest" when name is someone in the room.
func (set speakers) tag(line string) (rest string, self, ok bool) {
	name, rest, found := strings.Cut(line, ":")
	if !found || len(name) > 64 || strings.ContainsAny(name, "!?\"") {
		return line, false, false
	}
	self, ok = set[nameKeys(name)[0]]
	return strings.TrimSpace(rest), self, ok
}

// untranscript pulls the bot's own words out of a reply that drifted into
// being a chat log: the reply starts at the bot's own tag if there is one,
// leading echoes of the user and stage directions go, and it ends at the
// first line that carries anyone else's tag or a timestamp.
func untranscript(reply string, history []Message) string {
	set := newSpeakers(history)
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(strings.Replace(line, "`", "", 2))
		lines[i] = parenTag.ReplaceAllString(line, "")
	}

	foundSelf := false
	for i, line := range lines {
		if rest, self, ok := set.tag(line); ok && self {
			lines = lines[i:]
			lines[0], foundSelf = rest, true
			break
		}
	}
	// with no self line, an opening line wearing someone else's tag is the
	// model echoing a speaker before answering; keep what follows the tag.
	if !foundSelf {
		if rest, _, ok := set.tag(lines[0]); ok {
			lines[0] = rest
		}
	}

	recent := map[string]bool{}
	for i := len(history) - 1; i >= 0 && i >= len(history)-5; i-- {
		if history[i].Who != Self {
			recent[strings.ToLower(history[i].Content)] = true
			recent[strings.ToLower(stripSelfMentions(history[i].Content, history))] = true
		}
	}
	for len(lines) > 0 {
		line := lines[0]
		_, afterTag, _ := strings.Cut(line, ":")
		mention, _, _ := strings.Cut(strings.TrimSpace(afterTag), " ")
		pingsSelf := strings.HasPrefix(mention, "@") && set[nameKeys(mention)[0]]
		if line == "" || recent[strings.ToLower(line)] || pingsSelf || action.MatchString(line) || fakeOutput.MatchString(line) {
			lines = lines[1:]
			continue
		}
		break
	}

	for i, line := range lines {
		if _, self, ok := set.tag(line); (ok && !self) || timestampLine.MatchString(line) {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// stripSelfMentions drops the "@name" people use to get the bot's attention;
// left in, one reply in ten is the model complaining about being tagged. A
// message that was only the tag is left alone.
func stripSelfMentions(content string, history []Message) string {
	set := newSpeakers(history)
	fields := strings.Fields(content)
	kept := fields[:0]
	for _, f := range fields {
		if !(strings.HasPrefix(f, "@") && set[nameKeys(strings.TrimRight(f, ",.!?:;"))[0]]) {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return content
	}
	return strings.Join(kept, " ")
}

// wantsDetail reports whether the last person asked to have something
// explained.
func wantsDetail(history []Message) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Who != Self {
			c := strings.TrimSpace(stripSelfMentions(history[i].Content, history))
			return !smallTalk.MatchString(c) && detailAsk.MatchString(c)
		}
	}
	return false
}

// firstThought keeps the opening thought. The model writes a take, a line of
// elaboration, a line of caveats, and a question; a person sends the first.
// A one-word opener ("k.") also takes the line after it.
func firstThought(reply string) string {
	var kept []string
	words := 0
	for _, line := range strings.Split(reply, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
			if words += len(strings.Fields(line)); words >= 3 {
				break
			}
		}
	}
	return strings.Join(kept, "\n")
}

// unhook drops the engagement question a reply ends on. A trailing line that
// is a question goes; then the run of sentences the reply ends on goes if any
// of them is a hook. A question that is the whole reply stays.
func unhook(reply string) string {
	lines := strings.Split(reply, "\n")
	if last := lines[len(lines)-1]; len(lines) > 1 && (strings.HasSuffix(last, "?") || hook.MatchString(last)) {
		reply = strings.TrimSpace(strings.Join(lines[:len(lines)-1], "\n"))
	}

	starts := []int{0}
	for _, b := range sentence.FindAllStringIndex(reply, -1) {
		starts = append(starts, b[1])
	}
	cut, hooked := len(starts), false
	for i := len(starts) - 1; i > 0; i-- {
		s := strings.TrimSpace(reply[starts[i]:])
		if !strings.HasSuffix(s, "?") && !hook.MatchString(s) {
			break
		}
		hooked, cut = hooked || hook.MatchString(s), i
		reply = reply[:starts[i]]
	}
	if hooked {
		return strings.TrimSpace(reply[:starts[cut]])
	}
	return strings.TrimSpace(reply)
}

// clip cuts to limit runes at the last sentence end past the halfway mark,
// so the cut reads as the model stopping rather than the message breaking.
func clip(reply string, limit int) string {
	runes := []rune(reply)
	if len(runes) <= limit {
		return reply
	}
	head := string(runes[:limit])
	if i := strings.LastIndexAny(head, ".!?"); i > limit/2 {
		return strings.TrimSpace(head[:i+1])
	}
	return strings.TrimSpace(head)
}

// cleanReply turns a raw completion into a chat message: scratch work
// stripped, then the humanize pass.
func cleanReply(reply string, history []Message) string {
	reply = thinkBlock.ReplaceAllString(reply, "")
	reply = strings.ReplaceAll(reply, "<think>", "")
	return humanize(reply, history)
}

// humanize is the full pass, in dependency order: transcript noise first, then
// structure, then the length rules on what is left.
func humanize(reply string, history []Message) string {
	detail := wantsDetail(history)
	reply = untranscript(reply, history)
	reply = typography.Replace(structure.ReplaceAllString(reply, ""))
	reply = blankLines.ReplaceAllString(reply, "\n")
	limit := maxReplyRunes
	if !detail {
		reply, limit = firstThought(reply), maxTakeRunes
	}
	return clip(unhook(strings.TrimSpace(reply)), limit)
}

// draw asks the model for a reply and, when judge objects, asks again with a
// system nudge saying what went wrong. The last draw is kept either way, and
// a redraw that fails (the deadline, usually) keeps the draft before it.
func draw(ctx context.Context, client openai.Client, params openai.ChatCompletionNewParams, history []Message) (string, error) {
	var reply string
	for attempt := 0; ; attempt++ {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			if attempt > 0 {
				slog.Warn("redraw failed, keeping previous draft", slog.Any("error", err))
				return reply, nil
			}
			return "", err
		}
		if len(completion.Choices) == 0 {
			return "", fmt.Errorf("model returned no choices")
		}
		reply = cleanReply(completion.Choices[0].Message.Content, history)

		nudge := judge(reply)
		if nudge == "" || attempt >= maxRetries {
			return reply, nil
		}
		slog.Debug("redrawing reply", slog.Int("attempt", attempt), slog.String("draft", reply), slog.String("nudge", nudge))
		params.Messages = append(params.Messages, openai.SystemMessage(nudge))
	}
}

// judge returns the system nudge to redraw with, or "" if the reply is fine.
func judge(reply string) string {
	if strings.TrimSpace(reply) == "" {
		return "your last draft was empty. say something."
	}
	for _, s := range shingles(reply) {
		if exampleShingles[s] {
			return "your last draft reused an example line. those are tone, not a script. say something new, in your own words, about what they actually said."
		}
	}
	for _, r := range rejections {
		if r.re.MatchString(reply) {
			return r.nudge
		}
	}
	return ""
}
