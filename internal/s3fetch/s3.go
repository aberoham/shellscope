// Package s3fetch wraps the AWS S3 GetObject call for ProtoStreamV1
// recordings. The body is returned as an io.ReadCloser; callers must
// Close() it. We do not buffer the recording in memory because parts of
// it can be tens of MB and the parser is streaming.
package s3fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	api *s3.Client
}

func New(ctx context.Context, region string) (*Client, error) {
	opts := []func(*awscfg.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awscfg.WithRegion(region))
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &Client{api: s3.NewFromConfig(cfg)}, nil
}

// Get fetches an s3://bucket/key URI and returns a streaming reader plus
// the object size in bytes (0 if unknown).
func (c *Client) Get(ctx context.Context, uri string) (io.ReadCloser, int64, error) {
	bucket, key, err := parseS3URI(uri)
	if err != nil {
		return nil, 0, err
	}
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("s3 GetObject %s: %w", uri, err)
	}
	var size int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return out.Body, size, nil
}

func parseS3URI(uri string) (bucket, key string, err error) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", "", errors.New("not an s3:// uri: " + uri)
	}
	rest := strings.TrimPrefix(uri, "s3://")
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || slash == len(rest)-1 {
		return "", "", errors.New("missing key in s3 uri: " + uri)
	}
	return rest[:slash], rest[slash+1:], nil
}
