// Copyright IBM Corp. 2014, 2026
// SPDX-License-Identifier: BUSL-1.1

package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform/internal/ast"
)

// TestExamples_AWS_v3to4 verifies the v3→v4 example migrations run against the sample.
func TestExamples_AWS_v3to4(t *testing.T) {
	examplesDir := filepath.Join("examples", "aws-v3-to-v4")
	migrations, err := DiscoverMigrations(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	migrations = FilterMigrations(migrations, "v3to4/*")
	if len(migrations) == 0 {
		t.Fatal("no v3to4 migrations found")
	}

	src, err := os.ReadFile(filepath.Join(examplesDir, "sample.tf"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.ParseFile(src, "sample.tf", nil)
	if err != nil {
		t.Fatal(err)
	}
	mod := ast.NewModule([]*ast.File{f}, "", true, nil)

	for _, m := range migrations {
		if err := Execute(m, mod); err != nil {
			t.Fatalf("migration %s failed: %v", m.Name, err)
		}
	}

	got := string(mod.Bytes()["sample.tf"])

	// resource "aws_s3_bucket_object" should be renamed to "aws_s3_object"
	if strings.Contains(got, `resource "aws_s3_bucket_object"`) {
		t.Error("expected resource aws_s3_bucket_object renamed to aws_s3_object")
	}
	if !strings.Contains(got, `resource "aws_s3_object" "config"`) {
		t.Error("expected aws_s3_object resource")
	}

	// data "aws_s3_bucket_objects" should be renamed to "aws_s3_objects"
	if strings.Contains(got, `data "aws_s3_bucket_objects"`) {
		t.Error("expected data aws_s3_bucket_objects renamed to aws_s3_objects")
	}
	if !strings.Contains(got, `data "aws_s3_objects"`) {
		t.Error("expected data aws_s3_objects resource")
	}

	// Resource references should be rewritten
	if !strings.Contains(got, "aws_s3_object.config.id") {
		t.Error("expected references rewritten to aws_s3_object")
	}
	// NOTE: data source references (data.aws_s3_bucket_objects...) are NOT
	// rewritten because the traversal root is "data", not the resource type.
	// This is a known limitation.

	// Versioning should be extracted to new resource
	if !strings.Contains(got, `resource "aws_s3_bucket_versioning" "assets"`) {
		t.Error("expected aws_s3_bucket_versioning resource extracted")
	}

	// acl should be extracted to a standalone aws_s3_bucket_acl resource
	if !strings.Contains(got, `resource "aws_s3_bucket_acl" "assets"`) {
		t.Error("expected aws_s3_bucket_acl resource extracted")
	}

	// server_side_encryption_configuration should be extracted with nested blocks
	if !strings.Contains(got, `resource "aws_s3_bucket_server_side_encryption_configuration" "assets"`) {
		t.Error("expected aws_s3_bucket_server_side_encryption_configuration resource extracted")
	}
	if !strings.Contains(got, "rule {") {
		t.Error("expected rule block preserved in extracted server_side_encryption resource")
	}
	if !strings.Contains(got, "apply_server_side_encryption_by_default {") {
		t.Error("expected nested apply_server_side_encryption_by_default block preserved")
	}
}

// TestExamples_AWS_v4to5 verifies the v4→v5 example migrations run against the sample.
func TestExamples_AWS_v4to5(t *testing.T) {
	examplesDir := filepath.Join("examples", "aws-v4-to-v5")
	migrations, err := DiscoverMigrations(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	migrations = FilterMigrations(migrations, "v4to5/*")
	if len(migrations) == 0 {
		// Print all found migrations for debugging
		allMigs, _ := DiscoverMigrations(examplesDir)
		t.Fatalf("no v4to5 migrations found after filter. All discovered: %d", len(allMigs))
	}

	src, err := os.ReadFile(filepath.Join(examplesDir, "sample.tf"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.ParseFile(src, "sample.tf", nil)
	if err != nil {
		t.Fatal(err)
	}
	mod := ast.NewModule([]*ast.File{f}, "", true, nil)

	for _, m := range migrations {
		if err := Execute(m, mod); err != nil {
			t.Fatalf("migration %s failed: %v", m.Name, err)
		}
	}

	got := string(mod.Bytes()["sample.tf"])

	// EC2-Classic attributes should be removed
	if strings.Contains(got, "vpc_classic_link_id") {
		t.Error("expected vpc_classic_link_id removed")
	}
	// vpc_security_group_ids should survive
	if !strings.Contains(got, "vpc_security_group_ids") {
		t.Error("expected vpc_security_group_ids preserved")
	}

	// Autoscaling attachment renamed (check attribute assignment, not comments)
	if strings.Contains(got, "alb_target_group_arn   =") {
		t.Error("expected alb_target_group_arn renamed to lb_target_group_arn")
	}
	if !strings.Contains(got, "lb_target_group_arn") {
		t.Error("expected lb_target_group_arn present")
	}

	// Elasticache attributes renamed
	if strings.Contains(got, "replication_group_description") {
		t.Error("expected replication_group_description renamed to description")
	}
	if strings.Contains(got, "number_cache_clusters") {
		t.Error("expected number_cache_clusters renamed")
	}

	// cluster_mode block should be flattened
	if strings.Contains(got, "cluster_mode {") {
		t.Error("expected cluster_mode block flattened")
	}
	if !strings.Contains(got, "num_node_groups") {
		t.Error("expected num_node_groups at top level")
	}

	// DB instance name renamed
	if strings.Contains(got, `name           = "myappdb"`) {
		t.Error("expected 'name' renamed to 'db_name'")
	}

	// aws_db_security_group should be removed
	if strings.Contains(got, `resource "aws_db_security_group"`) {
		t.Error("expected aws_db_security_group removed")
	}
	if !strings.Contains(got, "FIXME: aws_db_security_group") {
		t.Error("expected FIXME comment for removed resource")
	}

	// RDS cluster without engine should get one added
	// (the first cluster has no engine, the second already has engine="aurora-mysql")
	if !strings.Contains(got, "aurora-mysql") {
		t.Error("expected existing aurora-mysql engine preserved")
	}
}

// TestExamples_AWS_v5to6 verifies the v5→v6 example migrations run against the sample.
func TestExamples_AWS_v5to6(t *testing.T) {
	examplesDir := filepath.Join("examples", "aws-v5-to-v6")
	migrations, err := DiscoverMigrations(examplesDir)
	if err != nil {
		t.Fatal(err)
	}
	migrations = FilterMigrations(migrations, "v5to6/*")
	if len(migrations) == 0 {
		t.Fatal("no v5to6 migrations found")
	}

	src, err := os.ReadFile(filepath.Join(examplesDir, "sample.tf"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := ast.ParseFile(src, "sample.tf", nil)
	if err != nil {
		t.Fatal(err)
	}
	mod := ast.NewModule([]*ast.File{f}, "", true, nil)

	for _, m := range migrations {
		if err := Execute(m, mod); err != nil {
			t.Fatalf("migration %s failed: %v", m.Name, err)
		}
	}

	got := string(mod.Bytes()["sample.tf"])

	// cpu_core_count and cpu_threads_per_core should be moved into cpu_options
	// (check attribute assignments, not comments)
	if strings.Contains(got, "cpu_core_count       =") {
		t.Error("expected cpu_core_count moved to cpu_options block")
	}
	if strings.Contains(got, "cpu_threads_per_core =") {
		t.Error("expected cpu_threads_per_core moved to cpu_options block")
	}
	if !strings.Contains(got, "cpu_options") {
		t.Error("expected cpu_options block created")
	}

	// Batch compute environment rename (check attr assignment, not comments)
	if strings.Contains(got, "compute_environment_name =") {
		t.Error("expected compute_environment_name renamed to name")
	}

	// OpsWorks should be removed (check resource block, not comments)
	if strings.Contains(got, `resource "aws_opsworks_stack"`) {
		t.Error("expected aws_opsworks_stack removed")
	}
	if !strings.Contains(got, "FIXME: aws_opsworks_stack") {
		t.Error("expected FIXME comment for opsworks removal")
	}

	// S3 bucket region renamed (check attr assignment, not comments)
	if strings.Contains(got, "region = ") && strings.Contains(got, `"my-data-bucket"`) {
		// The S3 bucket should have bucket_region, not region
		if !strings.Contains(got, "bucket_region") {
			t.Error("expected region renamed to bucket_region on aws_s3_bucket")
		}
	}

	// elastic_gpu_specifications can't be auto-removed (it's a block, not attribute),
	// so the migration adds a FIXME comment instead
	if !strings.Contains(got, "FIXME: elastic_gpu_specifications") {
		t.Error("expected FIXME comment for elastic_gpu_specifications")
	}
}
