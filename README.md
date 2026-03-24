# ami-direct

CLI tool imports a raw disk image as EBS Snapshot and registers it as an EC2 AMI.
Utilizes [EBS Direct API](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-accessing-snapshot.html)

```text
Usage of ami-direct:
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
        number of concurrent upload workers (1-10) (default 10)
```
