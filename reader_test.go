package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

func TestForEachChunkReadsAllChunks(t *testing.T) {
	const chunkSize = DefaultChunkSize

	dir := t.TempDir()
	path := filepath.Join(dir, "input.bin")

	totalSize := int(chunkSize*2 + 123)
	input := make([]byte, totalSize)
	for i := range input {
		input[i] = byte(i % 251)
	}

	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	var (
		mu     sync.Mutex
		chunks []Chunk
	)

	err := ForEachChunk(context.Background(), path, chunkSize, 3, func(_ context.Context, chunk Chunk) error {
		copyData := slices.Clone(chunk.Data)

		mu.Lock()
		defer mu.Unlock()
		chunks = append(chunks, Chunk{
			Index:  chunk.Index,
			Offset: chunk.Offset,
			Data:   copyData,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("process file: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	slices.SortFunc(chunks, func(a, b Chunk) int {
		switch {
		case a.Index < b.Index:
			return -1
		case a.Index > b.Index:
			return 1
		default:
			return 0
		}
	})

	if len(chunks[0].Data) != int(chunkSize) {
		t.Fatalf("chunk 0 size = %d, want %d", len(chunks[0].Data), chunkSize)
	}
	if len(chunks[1].Data) != int(chunkSize) {
		t.Fatalf("chunk 1 size = %d, want %d", len(chunks[1].Data), chunkSize)
	}
	if len(chunks[2].Data) != int(chunkSize) {
		t.Fatalf("chunk 2 size = %d, want %d", len(chunks[2].Data), chunkSize)
	}

	rebuilt := append(append(chunks[0].Data, chunks[1].Data...), chunks[2].Data...)
	if !slices.Equal(rebuilt[:len(input)], input) {
		t.Fatal("rebuilt data prefix does not match input")
	}
	for i, b := range rebuilt[len(input):] {
		if b != 0 {
			t.Fatalf("padding byte %d = %d, want 0", i, b)
		}
	}
}

func TestForEachChunkStopsOnProcessorError(t *testing.T) {
	const chunkSize = DefaultChunkSize

	dir := t.TempDir()
	path := filepath.Join(dir, "input.bin")
	data := make([]byte, chunkSize*2)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	wantErr := errors.New("boom")
	err := ForEachChunk(context.Background(), path, chunkSize, 2, func(_ context.Context, chunk Chunk) error {
		if chunk.Index == 1 {
			return wantErr
		}
		return nil
	})

	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestIsAllZero(t *testing.T) {
	if !isAllZero(make([]byte, 32)) {
		t.Fatal("all-zero slice should be reported as zero")
	}

	data := make([]byte, 32)
	data[17] = 1
	if isAllZero(data) {
		t.Fatal("slice with a non-zero byte should not be reported as zero")
	}
}

func TestVolumeSizeGiBForBytes(t *testing.T) {
	tests := []struct {
		sizeBytes int64
		wantGiB   int64
	}{
		{sizeBytes: 0, wantGiB: 1},
		{sizeBytes: 1, wantGiB: 1},
		{sizeBytes: 1024*1024*1024 - 1, wantGiB: 1},
		{sizeBytes: 1024 * 1024 * 1024, wantGiB: 1},
		{sizeBytes: 1024*1024*1024 + 1, wantGiB: 2},
	}

	for _, tc := range tests {
		if got := volumeSizeGiBForBytes(tc.sizeBytes); got != tc.wantGiB {
			t.Fatalf("volumeSizeGiBForBytes(%d) = %d, want %d", tc.sizeBytes, got, tc.wantGiB)
		}
	}
}
