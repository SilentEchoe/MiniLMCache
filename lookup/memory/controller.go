package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/SilentEchoe/MiniLMCache/lookup"
)

type Record struct {
	Key      lookup.ChunkKey
	Location lookup.Location
	Ready    bool
	Owner    string
}

type Reservation struct {
	ID       string
	EngineID string
	Hits     []lookup.Hit
}

type Controller struct {
	mu              sync.Mutex
	records         map[lookup.ChunkKey]Record
	reservations    map[string]Reservation
	nextReservation uint64
}

func NewController(records ...Record) *Controller {
	controller := &Controller{
		records:      make(map[lookup.ChunkKey]Record, len(records)),
		reservations: make(map[string]Reservation),
	}
	for _, record := range records {
		controller.records[record.Key] = record
	}
	return controller
}

func (c *Controller) Put(record Record) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records[record.Key] = record
}

func (c *Controller) LookupPrefix(ctx context.Context, chunks []lookup.Chunk) (lookup.Decision, error) {
	hits := make([]lookup.Hit, 0, len(chunks))

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			return lookup.Decision{}, ctx.Err()
		default:
		}

		record, ok := c.records[chunk.Key]
		if !ok || !record.Ready {
			break
		}

		hits = append(hits, lookup.Hit{
			Chunk:    chunk,
			Location: record.Location,
		})
	}

	return lookup.Decision{
		Hits:            hits,
		PrefixHitChunks: len(hits),
	}, nil
}

func (c *Controller) ReserveHits(ctx context.Context, engineID string, hits []lookup.Hit) (string, error) {
	if len(hits) == 0 {
		return "", nil
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.nextReservation++
	reservationID := fmt.Sprintf("resv-%04d", c.nextReservation)
	c.reservations[reservationID] = Reservation{
		ID:       reservationID,
		EngineID: engineID,
		Hits:     append([]lookup.Hit(nil), hits...),
	}

	return reservationID, nil
}

func (c *Controller) Reservation(id string) (Reservation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	reservation, ok := c.reservations[id]
	if !ok {
		return Reservation{}, false
	}

	reservation.Hits = append([]lookup.Hit(nil), reservation.Hits...)
	return reservation, true
}
