package llm

import (
	"sync"

	"github.com/disgoorg/snowflake/v2"
)

type Sender string

const (
	User           Sender = "user"
	Self           Sender = "self"
	maxHistorySize int    = 20
)

// A message in a greater conversation.
//
// The system prompt is not representable as that is owned by the provider
type Message struct {
	Who     Sender
	Name    string
	Content string
}

// A culmination of chat history sorted by channel id
type ChatHistory struct {
	lock      sync.RWMutex
	byChannel map[snowflake.ID]*ChannelHistory
}

// a history within a given channel
type ChannelHistory struct {
	lock     sync.Mutex
	head     int
	tail     int
	empty    bool
	maxSize  int
	messages []Message
}

type HistoryProvider interface {
	InsertMessage(channel snowflake.ID, sender Sender, name string, message string)
	OrderedHistory(channel snowflake.ID) []Message
	Empty(channel snowflake.ID) bool
}

func NewChatHistory() *ChatHistory {
	return &ChatHistory{byChannel: make(map[snowflake.ID]*ChannelHistory)}
}

func newChannelHistory() *ChannelHistory {
	return &ChannelHistory{
		head:     0,
		tail:     0,
		empty:    true,
		maxSize:  maxHistorySize,
		messages: make([]Message, maxHistorySize),
	}
}

func (provider *ChatHistory) InsertMessage(channel snowflake.ID, sender Sender, name string, message string) {
	provider.lock.RLock()
	history, ok := provider.byChannel[channel]
	provider.lock.RUnlock()

	// cache misses take the write lock, but hits only need a read lock, so existing conversations flow smoother
	if !ok {
		provider.lock.Lock()
		// another writer may have created this channel between the read and the write lock
		if history, ok = provider.byChannel[channel]; !ok {
			history = newChannelHistory()
			provider.byChannel[channel] = history
		}
		provider.lock.Unlock()
	}

	// locking per channel allows us to hit many channels at once
	history.lock.Lock()
	defer history.lock.Unlock()

	// the buffer is full, drop the oldest message to make room
	if history.head == history.tail && !history.empty {
		history.tail = (history.tail + 1) % history.maxSize
	}

	history.messages[history.head] = Message{Who: sender, Name: name, Content: message}
	history.head = (history.head + 1) % history.maxSize
	history.empty = false
}

func (provider *ChatHistory) OrderedHistory(channel snowflake.ID) []Message {
	provider.lock.RLock()
	history, ok := provider.byChannel[channel]
	provider.lock.RUnlock()
	if !ok {
		return []Message{}
	}

	history.lock.Lock()
	defer history.lock.Unlock()

	if history.empty {
		return []Message{}
	}

	// head == tail means the buffer is full, every other case is head running ahead of tail
	count := history.head - history.tail
	if count <= 0 {
		count += history.maxSize
	}

	ordered := make([]Message, 0, count)
	for i := 0; i < count; i++ {
		ordered = append(ordered, history.messages[(history.tail+i)%history.maxSize])
	}

	return ordered
}

func (provider *ChatHistory) Empty(channel snowflake.ID) bool {
	provider.lock.RLock()
	history, ok := provider.byChannel[channel]
	if !ok {
		provider.lock.RUnlock()
		return true
	}
	provider.lock.RUnlock()

	history.lock.Lock()
	defer history.lock.Unlock()
	return history.empty
}
