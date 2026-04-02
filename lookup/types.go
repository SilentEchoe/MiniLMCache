package lookup

import "context"

type TokenID int32

type ChunkKey string

type Status string

const (
	StatusHit        Status = "hit"
	StatusPartialHit Status = "partial_hit"
	StatusMiss       Status = "miss"
)

type Location string

const (
	LocationLocal  Location = "local"
	LocationRemote Location = "remote"
)

type Request struct {
	RequestID string
	EngineID  string
	Tokens    []TokenID
	ChunkSize int
}

type Chunk struct {
	Index int
	Start int
	End   int
	Key   ChunkKey
}

type Hit struct {
	Chunk    Chunk
	Location Location
}

type Decision struct {
	Hits            []Hit
	PrefixHitChunks int
}

type Result struct {
	Status               Status
	ReusablePrefixTokens int
	MissingFrom          int
	Hits                 []Hit
	NeedRetrieve         bool
	ReservationID        string
	Trace                []Event
}

type Controller interface {
	LookupPrefix(ctx context.Context, chunks []Chunk) (Decision, error)
	ReserveHits(ctx context.Context, engineID string, hits []Hit) (string, error)
}

type Service struct {
	Controller         Controller
	DefaultChunkSize   int
	EnableReservations bool
}
