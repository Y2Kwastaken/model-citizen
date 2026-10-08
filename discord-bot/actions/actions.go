package actions

import (
	"context"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
	"github.com/Y2Kwastaken/model-citizen/discord-bot/voice"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/snowflake/v2"
)

// something a user can ask the bot to do, run the same whether a slash
// command or the llm asked
type Action struct {
	Name        string
	Description string
	Params      []Param
	// returns the message for the user, an error only for real failures
	Run func(ctx context.Context, req Request) (string, error)
}

// a string option the action takes
type Param struct {
	Name        string
	Description string
	Required    bool
}

// who asked and where
type Request struct {
	Client *bot.Client
	Guild  snowflake.ID
	User   snowflake.ID
	Args   Args
	// the caller can't wait on slow work, long running actions should start
	// it and return straight away
	Background bool
}

// option values by name
type Args map[string]string

func (a Args) String(name string) string {
	return a[name]
}

// All returns every action the bot offers.
func All(services *state.GlobalServices) []Action {
	return []Action{
		joinAction(services),
		leaveAction(),
		playAction(services),
		stopAction(services),
		skipAction(services),
	}
}

// the refusal when the user and bot aren't in the same voice channel, ok when they are
func requireSameChannel(req Request) (string, bool) {
	_, status := voice.SameChannel(req.Client, req.User, req.Client.ID(), &req.Guild)
	switch status {
	case voice.Uncertain:
		return "uncertain channel status of you and bot", false
	case voice.FirstNotPresent:
		return "you must be in a voice channel to use this command", false
	case voice.SecondNotPresent:
		return "the bot must be in a voice channel to use this command", false
	case voice.BothPresent:
		return "", true
	case voice.DifferentPlace:
		return "this bot is already in another voice channel", false
	case voice.NeitherPresent:
		return "you are not in a voice channel", false
	}

	return "unknown voice channel status", false
}
