package tempo

// SongSourceID names the song being played as a tempo source, and
// SongSourceLabel is what the picker calls it.
//
// A song is a producer like any other: its beat comes from where the
// playhead is in the recording's own musical time, so the lights follow
// the record rather than a number typed in.
const (
	SongSourceID    = "song"
	SongSourceLabel = "Song"
)
