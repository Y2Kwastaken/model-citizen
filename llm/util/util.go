package util

// A source of information for a language model
type Source struct {
	// root id
	Id int64
	// where our source is coming from
	Where string
	// who is the owner of this source
	// empty for unattributed
	Who string
}
