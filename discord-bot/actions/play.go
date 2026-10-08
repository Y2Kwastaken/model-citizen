package actions

import (
	"context"
	"log/slog"

	"github.com/Y2Kwastaken/model-citizen/discord-bot/state"
)

func playAction(services *state.GlobalServices) Action {
	return Action{
		Name:        "play",
		Description: "plays the track that is linked or searched",
		Params: []Param{
			{
				Name:        "song",
				Description: "link to the song, or search terms",
				Required:    true,
			},
		},
		Run: playHandler(services),
	}
}

func playHandler(services *state.GlobalServices) func(context.Context, Request) (string, error) {
	return func(ctx context.Context, req Request) (string, error) {
		if msg, ok := requireSameChannel(req); !ok {
			return msg, nil
		}

		guildServices, ok := services.GetGuild(req.Guild)
		if !ok {
			return "could not fetch guild services", nil
		}

		query := req.Args.String("song")
		queue := func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, services.Config.Music.SearchTimeout)
			defer cancel()

			return guildServices.Player.Queue(query, ctx)
		}

		// searching can outlast the caller so it finishes on its own
		if req.Background {
			go func() {
				if err := queue(context.Background()); err != nil {
					slog.Error("queueing song", slog.String("song", query), slog.Any("error", err))
				}
			}()
			return "queueing " + query, nil
		}

		if err := queue(ctx); err != nil {
			return err.Error(), nil
		}
		return "queued " + query, nil
	}
}
