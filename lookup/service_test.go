package lookup_test

import (
	"context"
	"testing"

	"github.com/SilentEchoe/MiniLMCache/lookup"
	"github.com/SilentEchoe/MiniLMCache/lookup/memory"
)

func TestBuildChunksDeterministic(t *testing.T) {
	tokens := []lookup.TokenID{1, 2, 3, 4, 5, 6, 7, 8}

	chunksA, tailA, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}
	chunksB, tailB, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}

	if tailA != tailB {
		t.Fatalf("tail mismatch: %d != %d", tailA, tailB)
	}
	if len(chunksA) != len(chunksB) {
		t.Fatalf("chunk count mismatch: %d != %d", len(chunksA), len(chunksB))
	}
	for i := range chunksA {
		if chunksA[i].Key != chunksB[i].Key {
			t.Fatalf("chunk %d key mismatch: %q != %q", i, chunksA[i].Key, chunksB[i].Key)
		}
	}
}

func TestBuildChunksRejectsInvalidChunkSize(t *testing.T) {
	_, _, err := lookup.BuildChunks([]lookup.TokenID{1, 2, 3}, 0)
	if err == nil {
		t.Fatal("expected invalid chunk size error")
	}
}

func TestLookupRejectsEmptyTokens(t *testing.T) {
	service := lookup.Service{
		Controller:       memory.NewController(),
		DefaultChunkSize: 256,
	}

	_, err := service.Lookup(context.Background(), lookup.Request{})
	if err == nil {
		t.Fatal("expected empty tokens error")
	}
}

func TestLookupRejectsMissingEffectiveChunkSize(t *testing.T) {
	service := lookup.Service{
		Controller: memory.NewController(),
	}

	_, err := service.Lookup(context.Background(), lookup.Request{
		Tokens: []lookup.TokenID{1, 2, 3, 4},
	})
	if err == nil {
		t.Fatal("expected missing chunk size error")
	}
}

func TestLookupMissOnFirstChunk(t *testing.T) {
	service := lookup.Service{
		Controller:       memory.NewController(),
		DefaultChunkSize: 4,
	}

	result, err := service.Lookup(context.Background(), lookup.Request{
		RequestID: "req-miss",
		EngineID:  "engine-a",
		Tokens:    []lookup.TokenID{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if result.Status != lookup.StatusMiss {
		t.Fatalf("expected miss, got %s", result.Status)
	}
	if result.ReusablePrefixTokens != 0 {
		t.Fatalf("expected 0 reusable tokens, got %d", result.ReusablePrefixTokens)
	}
	if result.MissingFrom != 0 {
		t.Fatalf("expected missing_from 0, got %d", result.MissingFrom)
	}
}

func TestLookupPartialHitStopsAtFirstMiss(t *testing.T) {
	tokens := []lookup.TokenID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	chunks, _, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}

	controller := memory.NewController(
		memory.Record{Key: chunks[0].Key, Location: lookup.LocationLocal, Ready: true, Owner: "engine-a"},
		memory.Record{Key: chunks[1].Key, Location: lookup.LocationRemote, Ready: true, Owner: "engine-a"},
	)
	service := lookup.Service{
		Controller:         controller,
		DefaultChunkSize:   4,
		EnableReservations: true,
	}

	result, err := service.Lookup(context.Background(), lookup.Request{
		RequestID: "req-partial",
		EngineID:  "engine-b",
		Tokens:    tokens,
	})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if result.Status != lookup.StatusPartialHit {
		t.Fatalf("expected partial_hit, got %s", result.Status)
	}
	if result.ReusablePrefixTokens != 8 {
		t.Fatalf("expected 8 reusable tokens, got %d", result.ReusablePrefixTokens)
	}
	if result.MissingFrom != 8 {
		t.Fatalf("expected missing_from 8, got %d", result.MissingFrom)
	}
	if !result.NeedRetrieve {
		t.Fatal("expected need_retrieve to be true for remote hit")
	}
	if result.ReservationID == "" {
		t.Fatal("expected reservation id")
	}

	reservation, ok := controller.Reservation(result.ReservationID)
	if !ok {
		t.Fatalf("expected reservation %q to be recorded", result.ReservationID)
	}
	if reservation.EngineID != "engine-b" {
		t.Fatalf("expected reservation engine engine-b, got %q", reservation.EngineID)
	}
	if len(reservation.Hits) != 2 {
		t.Fatalf("expected 2 reserved hits, got %d", len(reservation.Hits))
	}
}

func TestLookupFullHitWithoutTail(t *testing.T) {
	tokens := []lookup.TokenID{10, 11, 12, 13, 14, 15, 16, 17}
	chunks, _, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}

	controller := memory.NewController(
		memory.Record{Key: chunks[0].Key, Location: lookup.LocationLocal, Ready: true, Owner: "engine-a"},
		memory.Record{Key: chunks[1].Key, Location: lookup.LocationLocal, Ready: true, Owner: "engine-a"},
	)
	service := lookup.Service{
		Controller:       controller,
		DefaultChunkSize: 4,
	}

	result, err := service.Lookup(context.Background(), lookup.Request{
		RequestID: "req-hit",
		EngineID:  "engine-c",
		Tokens:    tokens,
	})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if result.Status != lookup.StatusHit {
		t.Fatalf("expected hit, got %s", result.Status)
	}
	if result.MissingFrom != len(tokens) {
		t.Fatalf("expected missing_from %d, got %d", len(tokens), result.MissingFrom)
	}
	if result.NeedRetrieve {
		t.Fatal("expected need_retrieve to be false for local hits")
	}
	if result.ReservationID != "" {
		t.Fatalf("expected no reservation id, got %q", result.ReservationID)
	}
}

func TestLookupTailBecomesMissingRange(t *testing.T) {
	tokens := []lookup.TokenID{1, 2, 3, 4, 5, 6}
	chunks, _, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}

	controller := memory.NewController(
		memory.Record{Key: chunks[0].Key, Location: lookup.LocationLocal, Ready: true, Owner: "engine-a"},
	)
	service := lookup.Service{
		Controller:       controller,
		DefaultChunkSize: 4,
	}

	result, err := service.Lookup(context.Background(), lookup.Request{
		RequestID: "req-tail",
		EngineID:  "engine-d",
		Tokens:    tokens,
	})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if result.Status != lookup.StatusPartialHit {
		t.Fatalf("expected partial_hit, got %s", result.Status)
	}
	if result.ReusablePrefixTokens != 4 {
		t.Fatalf("expected 4 reusable tokens, got %d", result.ReusablePrefixTokens)
	}
	if result.MissingFrom != 4 {
		t.Fatalf("expected missing_from 4, got %d", result.MissingFrom)
	}
}

func TestLookupKeepsPrefixOnlyEvenIfLaterChunkExists(t *testing.T) {
	tokens := []lookup.TokenID{21, 22, 23, 24, 25, 26, 27, 28}
	chunks, _, err := lookup.BuildChunks(tokens, 4)
	if err != nil {
		t.Fatalf("BuildChunks returned error: %v", err)
	}

	controller := memory.NewController(
		memory.Record{Key: chunks[1].Key, Location: lookup.LocationRemote, Ready: true, Owner: "engine-z"},
	)
	service := lookup.Service{
		Controller:       controller,
		DefaultChunkSize: 4,
	}

	result, err := service.Lookup(context.Background(), lookup.Request{
		RequestID: "req-prefix",
		EngineID:  "engine-e",
		Tokens:    tokens,
	})
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}

	if result.Status != lookup.StatusMiss {
		t.Fatalf("expected miss because first chunk is missing, got %s", result.Status)
	}
	if len(result.Hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(result.Hits))
	}
}
