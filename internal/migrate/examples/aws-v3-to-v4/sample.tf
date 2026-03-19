# Sample Terraform configuration demonstrating AWS provider v3 patterns
# that need migration to v4.
#
# Run: terraform migrate run -migrations-dir=. "v3to4/*"

# --- S3 Bucket Object Rename ---
# v4 renames aws_s3_bucket_object to aws_s3_object.
# The migration renames the resource type and rewrites all references.

resource "aws_s3_bucket" "assets" {
  bucket = "my-app-assets"

  # v4 extracts versioning into a standalone aws_s3_bucket_versioning resource.
  # The extract_to_resource migration handles this automatically.
  versioning {
    enabled = true
  }

  # v4 extracts server_side_encryption_configuration into a standalone resource.
  server_side_encryption_configuration {
    rule {
      apply_server_side_encryption_by_default {
        sse_algorithm = "aws:kms"
      }
    }
  }

  # v4 extracts the acl argument from aws_s3_bucket into a standalone
  # aws_s3_bucket_acl resource.
  acl = "private"
}

resource "aws_s3_bucket_object" "config" {
  bucket  = aws_s3_bucket.assets.id
  key     = "config.json"
  content = jsonencode({ version = "1.0" })
}

resource "aws_s3_bucket_object" "readme" {
  bucket = aws_s3_bucket.assets.id
  key    = "README.md"
  source = "README.md"
}

data "aws_s3_bucket_objects" "all_objects" {
  bucket = aws_s3_bucket.assets.id
}

# References to aws_s3_bucket_object get rewritten to aws_s3_object
output "config_object_id" {
  value = aws_s3_bucket_object.config.id
}

output "readme_etag" {
  value = aws_s3_bucket_object.readme.etag
}

output "object_keys" {
  value = data.aws_s3_bucket_objects.all_objects.keys
}
