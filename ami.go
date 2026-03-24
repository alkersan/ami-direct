package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

const (
	defaultAMIWaitTimeout       = 30 * time.Minute
	defaultAMIRootDevice        = "/dev/xvda"
	defaultAMIVirtualization    = "hvm"
	defaultAMIDeregisterTimeout = 2 * time.Minute
	defaultAMIPollInterval      = 3 * time.Second
)

type RegisterAMIOptions struct {
	Name         string
	Architecture string
	Description  string
	Overwrite    bool
	Tags         []SnapshotTag
}

type RegisterAMIResult struct {
	ImageID string
	State   string
}

func RegisterAMIFromSnapshot(ctx context.Context, snapshotID string, opts RegisterAMIOptions) (RegisterAMIResult, error) {
	if snapshotID == "" {
		return RegisterAMIResult{}, fmt.Errorf("snapshot ID must not be empty")
	}
	if opts.Name == "" {
		return RegisterAMIResult{}, fmt.Errorf("AMI name must not be empty")
	}

	architecture, err := parseArchitecture(opts.Architecture)
	if err != nil {
		return RegisterAMIResult{}, err
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return RegisterAMIResult{}, fmt.Errorf("load AWS config: %w", err)
	}

	client := ec2.NewFromConfig(cfg)
	existingImages, err := findImagesByName(ctx, client, opts.Name)
	if err != nil {
		return RegisterAMIResult{}, err
	}
	if len(existingImages) > 0 {
		if !opts.Overwrite {
			return RegisterAMIResult{}, fmt.Errorf("AMI named %q already exists; use -ami-overwrite to replace it", opts.Name)
		}
		log.Printf("deregistering existing AMIs before overwrite")
		if err := deregisterImagesByName(ctx, client, existingImages); err != nil {
			return RegisterAMIResult{}, err
		}
		if err := waitForImagesDeletedByName(ctx, client, opts.Name, defaultAMIDeregisterTimeout); err != nil {
			return RegisterAMIResult{}, err
		}
	}

	log.Printf("registering AMI")
	registerInput := &ec2.RegisterImageInput{
		Architecture: architecture,
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{
			{
				DeviceName: aws.String(defaultAMIRootDevice),
				Ebs: &ec2types.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					SnapshotId:          aws.String(snapshotID),
					VolumeType:          ec2types.VolumeTypeGp3,
				},
			},
		},
		EnaSupport:         aws.Bool(true),
		Name:               aws.String(opts.Name),
		RootDeviceName:     aws.String(defaultAMIRootDevice),
		TagSpecifications:  toEC2ImageTags(opts.Tags),
		VirtualizationType: aws.String(defaultAMIVirtualization),
		BootMode:           ec2types.BootModeValuesUefi,
	}
	if opts.Description != "" {
		registerInput.Description = aws.String(opts.Description)
	}

	registerOutput, err := client.RegisterImage(ctx, registerInput)
	if err != nil {
		return RegisterAMIResult{}, fmt.Errorf("register AMI from snapshot %s: %w", snapshotID, err)
	}

	result := RegisterAMIResult{
		ImageID: aws.ToString(registerOutput.ImageId),
	}
	if result.ImageID == "" {
		return result, fmt.Errorf("register AMI from snapshot %s: empty image ID", snapshotID)
	}

	imageState, err := ec2.NewImageAvailableWaiter(client).WaitForOutput(
		ctx,
		&ec2.DescribeImagesInput{
			ImageIds: []string{result.ImageID},
		},
		defaultAMIWaitTimeout,
		func(o *ec2.ImageAvailableWaiterOptions) {
			o.MinDelay = 3 * time.Second
			o.MaxDelay = 5 * time.Second
		},
	)
	if err != nil {
		return result, fmt.Errorf("wait for AMI %s availability: %w", result.ImageID, err)
	}
	if len(imageState.Images) == 0 {
		return result, fmt.Errorf("wait for AMI %s availability: empty describe images response", result.ImageID)
	}

	result.State = string(imageState.Images[0].State)
	log.Printf("AMI available: id=%s", result.ImageID)
	return result, nil
}

func parseArchitecture(raw string) (ec2types.ArchitectureValues, error) {
	arch := ec2types.ArchitectureValues(raw)
	for _, value := range ec2types.ArchitectureValues("").Values() {
		if arch == value {
			return arch, nil
		}
	}

	return "", fmt.Errorf("invalid architecture %q", raw)
}

func toEC2ImageTags(tags []SnapshotTag) []ec2types.TagSpecification {
	if len(tags) == 0 {
		return nil
	}

	out := make([]ec2types.Tag, 0, len(tags))
	for _, tag := range tags {
		out = append(out, ec2types.Tag{
			Key:   aws.String(tag.Key),
			Value: aws.String(tag.Value),
		})
	}

	return []ec2types.TagSpecification{
		{
			ResourceType: ec2types.ResourceTypeImage,
			Tags:         out,
		},
	}
}

func findImagesByName(ctx context.Context, client *ec2.Client, name string) ([]ec2types.Image, error) {
	output, err := client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{name},
			},
		},
		IncludeDeprecated: aws.Bool(true),
		IncludeDisabled:   aws.Bool(true),
		Owners:            []string{"self"},
	})
	if err != nil {
		return nil, fmt.Errorf("describe existing AMIs named %q: %w", name, err)
	}

	return output.Images, nil
}

func deregisterImagesByName(ctx context.Context, client *ec2.Client, images []ec2types.Image) error {
	for _, image := range images {
		imageID := aws.ToString(image.ImageId)
		if imageID == "" {
			continue
		}

		if _, err := client.DeregisterImage(ctx, &ec2.DeregisterImageInput{
			DeleteAssociatedSnapshots: aws.Bool(true),
			ImageId:                   aws.String(imageID),
		}); err != nil {
			return fmt.Errorf("deregister AMI %s: %w", imageID, err)
		}
	}

	return nil
}

func waitForImagesDeletedByName(ctx context.Context, client *ec2.Client, name string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(defaultAMIPollInterval)
	defer ticker.Stop()

	for {
		images, err := findImagesByName(waitCtx, client, name)
		if err != nil {
			return err
		}
		if len(images) == 0 {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for existing AMIs named %q to disappear: %w", name, waitCtx.Err())
		case <-ticker.C:
		}
	}
}
