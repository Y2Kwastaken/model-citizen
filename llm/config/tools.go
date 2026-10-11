package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/Y2Kwastaken/model-citizen/llm/clients"
)

// the small model serious tools are handed to, every key is required once
// tools.toml is there
type Tools struct {
	// system prompt file, relative to the config directory like a personality's
	Prompt   string   `toml:"prompt"`
	Rotation Rotation `toml:"rotation"`
	// tried in order, the rotation moves off ones that are slow or down
	Clients []ChatClient `toml:"clients"`
}

// whether serious tools get their own model, validate guarantees a client once tools.toml is there
func (t Tools) Enabled() bool {
	return len(t.Clients) > 0
}

// builds every configured client, in rotation order
func (t Tools) ChatClients() []clients.ChatClient {
	return chatClients(t.Clients)
}

func (t Tools) validate() error {
	var errs []error

	if _, err := os.Stat(t.Prompt); err != nil {
		errs = append(errs, fmt.Errorf("tools.prompt: %w", err))
	}

	if err := t.Rotation.validate("tools"); err != nil {
		errs = append(errs, err)
	}

	if len(t.Clients) == 0 {
		errs = append(errs, errors.New("tools.clients needs at least one client"))
	}
	for i, client := range t.Clients {
		if err := client.validate(); err != nil {
			errs = append(errs, fmt.Errorf("tools.clients[%d] (%s): %w", i, client.Model, err))
		}
	}

	return errors.Join(errs...)
}
