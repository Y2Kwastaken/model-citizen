package model

type Clip struct {
	// Data is the whole file. The rotation may send it more than once.
	Data []byte
	// Format is the container as the endpoint names it: "wav", "flac", "ogg".
	Format string
	// Language is an ISO-639-1 hint. Empty lets the model detect.
	Language string
}
