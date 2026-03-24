package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
)

const (
	DefaultChunkSize  int64 = 512 * 1024
	MaxWorkers              = 20
	CostPer1000Chunks       = 0.006
)

func main() {
	var workers int
	var description string
	var name string
	var arch string
	var noAMI bool
	var noOverwrite bool
	var tags cliTags

	flag.IntVar(&workers, "workers", MaxWorkers, "number of concurrent upload workers (1-10)")
	flag.StringVar(&description, "description", "", "description for the snapshot and AMI")
	flag.StringVar(&name, "name", "", "AMI name; also used as the default Name tag")
	flag.StringVar(&arch, "arch", "x86_64", "AMI architecture")
	flag.BoolVar(&noAMI, "no-ami", false, "only create the snapshot; do not register an AMI")
	flag.BoolVar(&noOverwrite, "no-overwrite", false, "fail if an AMI with the same name already exists")
	flag.Var(&tags, "tag", "tag in Key=Value form; applied to the snapshot and AMI; repeatable")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-workers N] [-description TEXT] [-name NAME] [-no-ami] [-no-overwrite] [-arch ARCH] [-tag Key=Value] <input-file>\n", os.Args[0])
		os.Exit(2)
	}

	inputPath := flag.Arg(0)
	if workers < 1 || workers > MaxWorkers {
		fmt.Fprintf(os.Stderr, "error: workers must be between 1 and %d\n", MaxWorkers)
		os.Exit(2)
	}
	if !noAMI && name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required unless -no-ami is set")
		os.Exit(2)
	}

	parsedTags, err := parseTags(tags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	parsedTags = withDefaultNameTag(parsedTags, name)

	log.Printf("starting snapshot upload")
	result, err := UploadSnapshot(context.Background(), inputPath, UploadOptions{
		Workers:     workers,
		Description: description,
		Tags:        parsedTags,
	})

	printSummary(result.TotalChunks, result.DataChunks)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if noAMI {
		log.Printf("skipping AMI registration")
		return
	}

	log.Printf("starting AMI registration")
	_, err = RegisterAMIFromSnapshot(context.Background(), result.SnapshotID, RegisterAMIOptions{
		Name:         name,
		Architecture: arch,
		Description:  description,
		Overwrite:    !noOverwrite,
		Tags:         parsedTags,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func formatBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(n)
	unit := units[0]

	for i := 1; i < len(units) && value >= 1024; i++ {
		value /= 1024
		unit = units[i]
	}

	if unit == "B" {
		return fmt.Sprintf("%d %s", n, unit)
	}
	if math.Abs(value-math.Round(value)) < 0.05 {
		return fmt.Sprintf("%.0f %s", value, unit)
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func printSummary(totalChunks int64, dataChunks int64) {
	zeroChunks := totalChunks - dataChunks
	totalSizeBytes := totalChunks * DefaultChunkSize
	estimatedCost := float64(dataChunks) * CostPer1000Chunks / 1000

	log.Printf(
		"summary: %d req %s total size, %d zero blocks, cost $%.3f\n",
		dataChunks,
		formatBytes(totalSizeBytes),
		zeroChunks,
		estimatedCost,
	)
}

type cliTags []string

func (t *cliTags) String() string {
	return strings.Join(*t, ",")
}

func (t *cliTags) Set(value string) error {
	*t = append(*t, value)
	return nil
}

func parseTags(rawTags []string) ([]SnapshotTag, error) {
	if len(rawTags) == 0 {
		return nil, nil
	}

	tags := make([]SnapshotTag, 0, len(rawTags))
	for _, rawTag := range rawTags {
		key, value, ok := strings.Cut(rawTag, "=")
		if !ok {
			return nil, fmt.Errorf("invalid tag %q: expected Key=Value", rawTag)
		}
		if key == "" {
			return nil, fmt.Errorf("invalid tag %q: key must not be empty", rawTag)
		}

		tags = append(tags, SnapshotTag{
			Key:   key,
			Value: value,
		})
	}

	return tags, nil
}

func withDefaultNameTag(tags []SnapshotTag, name string) []SnapshotTag {
	if name == "" {
		return tags
	}

	for _, tag := range tags {
		if tag.Key == "Name" {
			return tags
		}
	}

	return append(tags, SnapshotTag{
		Key:   "Name",
		Value: name,
	})
}
