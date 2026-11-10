package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/disgoorg/disgo/gateway"
)

const (
	// discord resets the bot's token after 1000 logins in a day, a healthy bot needs a handful
	maxDailyLogins = 50
	loginWindow    = 24 * time.Hour
)

var errLoginsSpent = errors.New("gateway logins for the day are spent")

var _ gateway.IdentifyRateLimiter = (*LoginBudget)(nil)

// counts gateway logins (identifies, not resumes) and refuses them past maxDailyLogins
type LoginBudget struct {
	// receives once the budget runs out so main can stop the bot
	Spent chan struct{}
	lock  sync.Mutex
	// times of the logins inside loginWindow, oldest first
	logins []time.Time
}

func NewLoginBudget() *LoginBudget {
	return &LoginBudget{Spent: make(chan struct{}, 1)}
}

// called by the gateway before every login
func (b *LoginBudget) Wait(_ context.Context, _ int) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	now := time.Now()
	for len(b.logins) > 0 && now.Sub(b.logins[0]) > loginWindow {
		b.logins = b.logins[1:]
	}

	if len(b.logins) >= maxDailyLogins {
		select {
		case b.Spent <- struct{}{}:
		default:
		}
		return errLoginsSpent
	}

	b.logins = append(b.logins, now)
	return nil
}

func (b *LoginBudget) Unlock(_ int) {}

func (b *LoginBudget) Close(_ context.Context) {}
