package lookup

import (
	"context"
	"fmt"
)

func (s *Service) Lookup(ctx context.Context, req Request) (Result, error) {
	if s.Controller == nil {
		return Result{}, fmt.Errorf("lookup: controller is required")
	}
	if len(req.Tokens) == 0 {
		return Result{}, fmt.Errorf("lookup: tokens must not be empty")
	}

	chunkSize := req.ChunkSize
	if chunkSize <= 0 {
		chunkSize = s.DefaultChunkSize
	}
	if chunkSize <= 0 {
		return Result{}, fmt.Errorf("lookup: chunk size must be greater than 0")
	}

	trace := []Event{
		{
			Step:   "request_validated",
			Detail: fmt.Sprintf("request_id=%q engine_id=%q token_count=%d chunk_size=%d", req.RequestID, req.EngineID, len(req.Tokens), chunkSize),
		},
	}

	chunks, tailStart, err := BuildChunks(req.Tokens, chunkSize)
	if err != nil {
		return Result{}, err
	}
	trace = append(trace, Event{
		Step:   "chunks_prepared",
		Detail: fmt.Sprintf("full_chunks=%d tail_start=%d", len(chunks), tailStart),
	})

	decision, err := s.Controller.LookupPrefix(ctx, chunks)
	if err != nil {
		return Result{}, err
	}
	if decision.PrefixHitChunks < 0 || decision.PrefixHitChunks > len(chunks) {
		return Result{}, fmt.Errorf("lookup: controller returned invalid prefix hit count %d", decision.PrefixHitChunks)
	}
	if len(decision.Hits) != decision.PrefixHitChunks {
		return Result{}, fmt.Errorf("lookup: controller returned %d hits but prefix hit count %d", len(decision.Hits), decision.PrefixHitChunks)
	}

	trace = append(trace, Event{
		Step:   "lookup_completed",
		Detail: fmt.Sprintf("prefix_hit_chunks=%d", decision.PrefixHitChunks),
	})

	reservationID := ""
	if s.EnableReservations && len(decision.Hits) > 0 {
		reservationID, err = s.Controller.ReserveHits(ctx, req.EngineID, decision.Hits)
		if err != nil {
			return Result{}, err
		}
		trace = append(trace, Event{
			Step:   "hits_reserved",
			Detail: fmt.Sprintf("reservation_id=%q hit_count=%d", reservationID, len(decision.Hits)),
		})
	}

	result := Result{
		Status:               deriveStatus(req.Tokens, chunks, tailStart, decision.PrefixHitChunks, chunkSize),
		ReusablePrefixTokens: decision.PrefixHitChunks * chunkSize,
		MissingFrom:          deriveMissingFrom(req.Tokens, chunks, tailStart, decision.PrefixHitChunks),
		Hits:                 append([]Hit(nil), decision.Hits...),
		NeedRetrieve:         needRetrieve(decision.Hits),
		ReservationID:        reservationID,
	}

	trace = append(trace, Event{
		Step:   "result_built",
		Detail: fmt.Sprintf("status=%s reusable_prefix_tokens=%d missing_from=%d need_retrieve=%t", result.Status, result.ReusablePrefixTokens, result.MissingFrom, result.NeedRetrieve),
	})
	result.Trace = trace

	return result, nil
}

func deriveStatus(tokens []TokenID, chunks []Chunk, tailStart int, prefixHitChunks int, chunkSize int) Status {
	reusablePrefixTokens := prefixHitChunks * chunkSize
	if reusablePrefixTokens == 0 {
		return StatusMiss
	}
	if deriveMissingFrom(tokens, chunks, tailStart, prefixHitChunks) < len(tokens) {
		return StatusPartialHit
	}
	return StatusHit
}

func deriveMissingFrom(tokens []TokenID, chunks []Chunk, tailStart int, prefixHitChunks int) int {
	if len(chunks) == 0 {
		return 0
	}
	if prefixHitChunks < len(chunks) {
		return chunks[prefixHitChunks].Start
	}
	if tailStart < len(tokens) {
		return tailStart
	}
	return len(tokens)
}

func needRetrieve(hits []Hit) bool {
	for _, hit := range hits {
		if hit.Location != LocationLocal {
			return true
		}
	}
	return false
}
