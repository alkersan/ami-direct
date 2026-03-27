# ami-direct

CLI tool imports a raw disk image as an EBS snapshot and registers it as an EC2 AMI.
Uses [EBS Direct API](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-accessing-snapshot.html).

### Context

I built this prototype because local OS image creation had stopped being the slow part.
[mkosi](https://github.com/systemd/mkosi) can build a tailored Linux disk image in a matter of seconds,
but pushing those into AWS as AMIs still took several minutes per attempt:
- [import-snapshot](https://docs.aws.amazon.com/vm-import/latest/userguide/vmimport-import-snapshot.html) is reliable
and requires very little setup, but was too slow for a tight build-run-debug cycle,
sometimes up to 12 minutes even for small images.
- the faster `S3 -> tiny EC2 -> EBS -> snapshot`[^1] path still meant waiting a few minutes
after every kernel option or boot script change

The EBS Direct API provides a faster path: stream the disk blocks straight into a snapshot,
then register the AMI, typically in tens of seconds.

The tradeoff is cost: [PutSnapshotBlock pricing](https://docs.aws.amazon.com/ebs/latest/userguide/ebsapi-pricing.html)
is $0.006 per 1,000 requests, or about 1.2 cents per GiB written.

[^1]: [EBS Surrogate](https://developer.hashicorp.com/packer/integrations/hashicorp/amazon/latest/components/builder/ebssurrogate) as Packer calls it

### Usage

```text
Usage: ami-direct [flags] <input-file>

  <input-file> must be a raw disk image

Flags:
  -arch string
        AMI architecture (default "x86_64")
  -description string
        description for the snapshot and AMI
  -name string
        AMI name; also used as the default Name tag
  -no-ami
        only create the snapshot; do not register an AMI
  -no-overwrite
        fail if an AMI with the same name already exists
  -tag value
        tag in Key=Value form; applied to the snapshot and AMI; repeatable
  -workers int
        number of concurrent upload workers (1-20) (default 20)
```
