// Package storage is a thin wrapper around gocloud.dev/blob so the rest of
// the codebase can speak one URL-driven object storage API regardless of
// where it is deployed.
package storage

import (
	"context"
	"fmt"

	"gocloud.dev/blob"

	// Driver registrations. Each blank import wires its URL scheme into
	// blob.DefaultURLMux so OpenBucket can dispatch by URL.
	_ "gocloud.dev/blob/fileblob"
	_ "gocloud.dev/blob/memblob"
	_ "gocloud.dev/blob/s3blob"
)

// OpenBucket dispatches to a gocloud.dev/blob driver by URL scheme.
// Supported URLs:
//
//	s3://<bucket>?region=<region> AWS S3 or any S3-compatible (SeaweedFS,
//	                                MinIO, R2). Add &endpoint=... for non-AWS.
//	file:///<absolute-dir> local filesystem, for single-container demo
//	mem:// in-process, for tests
//
// More drivers (gcsblob, azureblob, ...) can be enabled by adding their blank imports to this file.
func OpenBucket(ctx context.Context, url string) (*blob.Bucket, error) {
	b, err := blob.OpenBucket(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open bucket %q: %w", url, err)
	}
	return b, nil
}
