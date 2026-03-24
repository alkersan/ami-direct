package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ebs"
	ebstypes "github.com/aws/aws-sdk-go-v2/service/ebs/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

const (
	defaultTimeoutMinutes      = 10
	defaultSnapshotWaitTimeout = 30 * time.Minute
)

type UploadOptions struct {
	Workers     int
	Description string
	Tags        []SnapshotTag
}

type UploadResult struct {
	SnapshotID  string
	Status      string
	TotalChunks int64
	DataChunks  int64
}

type SnapshotTag struct {
	Key   string
	Value string
}

func UploadSnapshot(ctx context.Context, path string, opts UploadOptions) (UploadResult, error) {
	if opts.Workers < 1 {
		return UploadResult{}, fmt.Errorf("workers must be at least 1")
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return UploadResult{}, fmt.Errorf("stat input file: %w", err)
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return UploadResult{}, fmt.Errorf("load AWS config: %w", err)
	}
	ebsClient := ebs.NewFromConfig(cfg)

	startInput := &ebs.StartSnapshotInput{
		Timeout:    aws.Int32(defaultTimeoutMinutes),
		VolumeSize: aws.Int64(volumeSizeGiBForBytes(fileInfo.Size())),
		Tags:       toEBSTags(opts.Tags),
	}
	if opts.Description != "" {
		startInput.Description = aws.String(opts.Description)
	}

	startOutput, err := ebsClient.StartSnapshot(ctx, startInput)
	if err != nil {
		return UploadResult{}, fmt.Errorf("start snapshot: %w", err)
	}

	snapshotID := aws.ToString(startOutput.SnapshotId)
	if snapshotID == "" {
		return UploadResult{}, fmt.Errorf("start snapshot returned empty snapshot ID")
	}
	log.Printf("snapshot started: %s %d GiB", snapshotID, volumeSizeGiBForBytes(fileInfo.Size()))

	var totalChunks atomic.Int64
	var zeroChunks atomic.Int64
	var dataChunks atomic.Int64

	err = ForEachChunk(ctx, path, DefaultChunkSize, opts.Workers, func(ctx context.Context, chunk Chunk) error {
		totalChunks.Add(1)
		if isAllZero(chunk.Data) {
			zeroChunks.Add(1)
			return nil
		}

		sum := sha256.Sum256(chunk.Data)
		sumBase64 := base64.StdEncoding.EncodeToString(sum[:])

		output, err := ebsClient.PutSnapshotBlock(ctx, &ebs.PutSnapshotBlockInput{
			BlockData:         bytes.NewReader(chunk.Data),
			BlockIndex:        aws.Int32(int32(chunk.Index)),
			Checksum:          aws.String(sumBase64),
			ChecksumAlgorithm: ebstypes.ChecksumAlgorithmChecksumAlgorithmSha256,
			DataLength:        aws.Int32(int32(len(chunk.Data))),
			SnapshotId:        aws.String(snapshotID),
		})
		if err != nil {
			return fmt.Errorf("put snapshot block %d: %w", chunk.Index, err)
		}
		if checksum := aws.ToString(output.Checksum); checksum != "" && checksum != sumBase64 {
			return fmt.Errorf("put snapshot block %d: checksum mismatch", chunk.Index)
		}

		dataChunks.Add(1)
		return nil
	})
	if err != nil {
		return UploadResult{
			SnapshotID:  snapshotID,
			TotalChunks: totalChunks.Load(),
			DataChunks:  dataChunks.Load(),
		}, fmt.Errorf("upload snapshot %s: %w", snapshotID, err)
	}

	dataChunkCount := dataChunks.Load()
	log.Printf("snapshot block upload complete")
	completeOutput, err := ebsClient.CompleteSnapshot(ctx, &ebs.CompleteSnapshotInput{
		ChangedBlocksCount: aws.Int32(int32(dataChunkCount)),
		SnapshotId:         aws.String(snapshotID),
	})
	if err != nil {
		return UploadResult{
			SnapshotID:  snapshotID,
			TotalChunks: totalChunks.Load(),
			DataChunks:  dataChunkCount,
		}, fmt.Errorf("complete snapshot %s: %w", snapshotID, err)
	}

	result := UploadResult{
		SnapshotID:  snapshotID,
		Status:      string(completeOutput.Status),
		TotalChunks: totalChunks.Load(),
		DataChunks:  dataChunkCount,
	}

	ec2Client := ec2.NewFromConfig(cfg)
	log.Printf("waiting for snapshot completion")

	snapshotState, err := ec2.NewSnapshotCompletedWaiter(ec2Client).WaitForOutput(
		ctx,
		&ec2.DescribeSnapshotsInput{
			SnapshotIds: []string{snapshotID},
		},
		defaultSnapshotWaitTimeout,
		func(o *ec2.SnapshotCompletedWaiterOptions) {
			o.MinDelay = 3 * time.Second
			o.MaxDelay = 5 * time.Second
		},
	)

	if err != nil {
		return result, fmt.Errorf("wait for snapshot %s completion: %w", snapshotID, err)
	}

	if len(snapshotState.Snapshots) == 0 {
		return result, fmt.Errorf("wait for snapshot %s completion: empty describe snapshots response", snapshotID)
	}

	result.Status = string(snapshotState.Snapshots[0].State)
	log.Printf("snapshot completed")
	return result, nil
}

func isAllZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func volumeSizeGiBForBytes(sizeBytes int64) int64 {
	const gib = 1024 * 1024 * 1024

	if sizeBytes <= 0 {
		return 1
	}

	return (sizeBytes + gib - 1) / gib
}

func toEBSTags(tags []SnapshotTag) []ebstypes.Tag {
	if len(tags) == 0 {
		return nil
	}

	out := make([]ebstypes.Tag, 0, len(tags))
	for _, tag := range tags {
		out = append(out, ebstypes.Tag{
			Key:   aws.String(tag.Key),
			Value: aws.String(tag.Value),
		})
	}

	return out
}
