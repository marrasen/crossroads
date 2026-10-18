package tempo

// SeratoSourceID names Serato as a tempo source, and SeratoSourceLabel
// is what the ranking calls it when no track is loaded.
//
// There is one source for Serato rather than one for each deck: it
// follows whichever deck is audible, so a DJ never reaches for the
// ranking mid-set, and the ranking itself stays the same from one set
// to the next.
const (
	SeratoSourceID    = "serato"
	SeratoSourceLabel = "Serato"
)
