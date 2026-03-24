package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

type Chunk struct {
	Index  int64
	Offset int64
	Data   []byte
}

type Processor func(context.Context, Chunk) error

func ForEachChunk(ctx context.Context, path string, chunkSize int64, workers int, processor Processor) error {
	if chunkSize < 1 {
		return fmt.Errorf("chunk size must be at least 1")
	}
	if processor == nil {
		return fmt.Errorf("processor must not be nil")
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Chunk)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for chunk := range jobs {
				if ctx.Err() != nil {
					return
				}

				if err := processor(ctx, chunk); err != nil {
					sendError(errCh, fmt.Errorf("process chunk %d: %w", chunk.Index, err))
					cancel()
					return
				}
			}
		})
	}

readLoop:
	for index := int64(0); ; index++ {
		chunk, err := readChunk(file, index, chunkSize)
		if err == io.EOF {
			break
		}
		if err != nil {
			sendError(errCh, fmt.Errorf("read chunk %d: %w", index, err))
			cancel()
			break
		}

		select {
		case <-ctx.Done():
			break readLoop
		case jobs <- chunk:
		}
	}
	close(jobs)
	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		if ctx.Err() == context.Canceled {
			return ctx.Err()
		}
		return nil
	}
}

func readChunk(file *os.File, index int64, chunkSize int64) (Chunk, error) {
	buf := make([]byte, chunkSize)
	_, err := io.ReadFull(file, buf)
	if err == io.EOF {
		return Chunk{}, io.EOF
	}
	if err != nil && err != io.ErrUnexpectedEOF {
		return Chunk{}, err
	}

	return Chunk{
		Index:  index,
		Offset: index * chunkSize,
		Data:   buf,
	}, nil
}

func sendError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}
