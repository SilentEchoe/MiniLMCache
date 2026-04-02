package main

import (
	"context"
	"fmt"
	"log"

	"github.com/SilentEchoe/MiniLMCache/lookup"
	"github.com/SilentEchoe/MiniLMCache/lookup/memory"
)

const demoChunkSize = 4

type scenario struct {
	name       string
	request    lookup.Request
	controller *memory.Controller
}

func main() {
	ctx := context.Background()

	scenarios, err := buildScenarios()
	if err != nil {
		log.Fatalf("build demo scenarios: %v", err)
	}

	for _, scenario := range scenarios {
		service := lookup.Service{
			Controller:         scenario.controller,
			DefaultChunkSize:   256,
			EnableReservations: true,
		}

		result, err := service.Lookup(ctx, scenario.request)
		if err != nil {
			log.Fatalf("%s: %v", scenario.name, err)
		}

		printScenario(scenario.name, scenario.request, result)
		fmt.Println()
	}
}

func buildScenarios() ([]scenario, error) {
	fullHitTokens := []lookup.TokenID{1, 2, 3, 4, 5, 6, 7, 8}
	partialHitTokens := []lookup.TokenID{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22}
	firstMissTokens := []lookup.TokenID{31, 32, 33, 34, 35, 36, 37, 38}

	fullHitController, err := controllerWithChunkIndexes(fullHitTokens, []seed{
		{index: 0, location: lookup.LocationLocal, owner: "engine-a"},
		{index: 1, location: lookup.LocationRemote, owner: "engine-a"},
	})
	if err != nil {
		return nil, err
	}

	partialHitController, err := controllerWithChunkIndexes(partialHitTokens, []seed{
		{index: 0, location: lookup.LocationLocal, owner: "engine-b"},
		{index: 1, location: lookup.LocationRemote, owner: "engine-b"},
	})
	if err != nil {
		return nil, err
	}

	firstMissController, err := controllerWithChunkIndexes(firstMissTokens, []seed{
		{index: 1, location: lookup.LocationRemote, owner: "engine-c"},
	})
	if err != nil {
		return nil, err
	}

	return []scenario{
		{
			name: "full_hit",
			request: lookup.Request{
				RequestID: "demo-hit",
				EngineID:  "engine-x",
				Tokens:    fullHitTokens,
				ChunkSize: demoChunkSize,
			},
			controller: fullHitController,
		},
		{
			name: "partial_hit",
			request: lookup.Request{
				RequestID: "demo-partial",
				EngineID:  "engine-y",
				Tokens:    partialHitTokens,
				ChunkSize: demoChunkSize,
			},
			controller: partialHitController,
		},
		{
			name: "first_miss",
			request: lookup.Request{
				RequestID: "demo-miss",
				EngineID:  "engine-z",
				Tokens:    firstMissTokens,
				ChunkSize: demoChunkSize,
			},
			controller: firstMissController,
		},
	}, nil
}

type seed struct {
	index    int
	location lookup.Location
	owner    string
}

func controllerWithChunkIndexes(tokens []lookup.TokenID, seeds []seed) (*memory.Controller, error) {
	chunks, _, err := lookup.BuildChunks(tokens, demoChunkSize)
	if err != nil {
		return nil, err
	}

	records := make([]memory.Record, 0, len(seeds))
	for _, seed := range seeds {
		records = append(records, memory.Record{
			Key:      chunks[seed.index].Key,
			Location: seed.location,
			Ready:    true,
			Owner:    seed.owner,
		})
	}

	return memory.NewController(records...), nil
}

func printScenario(name string, req lookup.Request, result lookup.Result) {
	fmt.Printf("== %s ==\n", name)
	fmt.Printf("request_id=%s engine_id=%s tokens=%v chunk_size=%d\n", req.RequestID, req.EngineID, req.Tokens, req.ChunkSize)
	fmt.Printf("status=%s reusable_prefix_tokens=%d missing_from=%d need_retrieve=%t reservation_id=%q\n", result.Status, result.ReusablePrefixTokens, result.MissingFrom, result.NeedRetrieve, result.ReservationID)

	fmt.Println("hits:")
	if len(result.Hits) == 0 {
		fmt.Println("  (none)")
	} else {
		for _, hit := range result.Hits {
			fmt.Printf("  chunk=%d range=[%d:%d] location=%s key=%s\n", hit.Chunk.Index, hit.Chunk.Start, hit.Chunk.End, hit.Location, shortKey(hit.Chunk.Key))
		}
	}

	fmt.Println("trace:")
	for _, event := range result.Trace {
		fmt.Printf("  - %s: %s\n", event.Step, event.Detail)
	}
}

func shortKey(key lookup.ChunkKey) string {
	if len(key) <= 12 {
		return string(key)
	}
	return string(key[:12])
}
