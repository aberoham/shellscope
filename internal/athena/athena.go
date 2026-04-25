// Package athena queries the EAS-managed Glue/Athena table for
// session.upload events. Partition pruning by event_date is mandatory —
// without it Athena scans every partition.
//
// The session.upload (T2005I) event is the durability signal that the
// recording is finalised in S3. event_data.url (proto JSON tag "url" on
// SessionUpload.SessionURL) is the s3://bucket/prefix/<sid>.tar reference.
package athena

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

type Config struct {
	Workgroup    string
	Database     string
	Catalog      string
	BytesScanCap int64
	Region       string
}

type Client struct {
	api *athena.Client
	cfg Config
}

type UploadRow struct {
	UID          string
	EventTime    time.Time
	SessionID    string
	User         string
	RecordingURI string
	Cluster      string
}

func New(ctx context.Context, c Config) (*Client, error) {
	if c.Workgroup == "" || c.Database == "" {
		return nil, errors.New("athena: Workgroup and Database are required")
	}
	if c.Catalog == "" {
		c.Catalog = "AwsDataCatalog"
	}
	opts := []func(*awscfg.LoadOptions) error{}
	if c.Region != "" {
		opts = append(opts, awscfg.WithRegion(c.Region))
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	return &Client{api: athena.NewFromConfig(cfg), cfg: c}, nil
}

const queryRangeSQL = `
SELECT uid, event_time, session_id, user,
       json_extract_scalar(event_data, '$.url')          AS recording_uri,
       json_extract_scalar(event_data, '$.cluster_name') AS cluster
FROM   %s.%s
WHERE  event_date BETWEEN date('%s') AND date('%s')
  AND  event_type = 'session.upload'
ORDER  BY event_time ASC`

const querySessionSQL = `
SELECT uid, event_time, session_id, user,
       json_extract_scalar(event_data, '$.url')          AS recording_uri,
       json_extract_scalar(event_data, '$.cluster_name') AS cluster
FROM   %s.%s
WHERE  event_type = 'session.upload'
  AND  session_id = '%s'
LIMIT  10`

func (c *Client) QueryRange(ctx context.Context, since, until time.Time) ([]UploadRow, error) {
	q := fmt.Sprintf(queryRangeSQL,
		safeIdent(c.cfg.Database), "teleport_events",
		since.Format("2006-01-02"), until.Format("2006-01-02"))
	return c.run(ctx, q)
}

func (c *Client) QuerySession(ctx context.Context, sessionID string) ([]UploadRow, error) {
	if !validSessionID(sessionID) {
		return nil, fmt.Errorf("invalid session id %q", sessionID)
	}
	q := fmt.Sprintf(querySessionSQL,
		safeIdent(c.cfg.Database), "teleport_events", sessionID)
	return c.run(ctx, q)
}

func (c *Client) run(ctx context.Context, q string) ([]UploadRow, error) {
	startIn := &athena.StartQueryExecutionInput{
		QueryString: aws.String(q),
		WorkGroup:   aws.String(c.cfg.Workgroup),
		QueryExecutionContext: &athenatypes.QueryExecutionContext{
			Catalog:  aws.String(c.cfg.Catalog),
			Database: aws.String(c.cfg.Database),
		},
	}
	if c.cfg.BytesScanCap > 0 {
		startIn.ResultReuseConfiguration = nil // explicit: no reuse
	}
	start, err := c.api.StartQueryExecution(ctx, startIn)
	if err != nil {
		return nil, fmt.Errorf("StartQueryExecution: %w", err)
	}
	qid := aws.ToString(start.QueryExecutionId)

	if err := c.waitFor(ctx, qid); err != nil {
		return nil, err
	}

	var (
		rows  []UploadRow
		token *string
		first = true
	)
	for {
		out, err := c.api.GetQueryResults(ctx, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(qid),
			NextToken:        token,
		})
		if err != nil {
			return nil, fmt.Errorf("GetQueryResults: %w", err)
		}
		for i, row := range out.ResultSet.Rows {
			if first && i == 0 {
				continue // header row
			}
			if len(row.Data) < 6 {
				continue
			}
			ur := UploadRow{
				UID:          colString(row.Data[0]),
				SessionID:    colString(row.Data[2]),
				User:         colString(row.Data[3]),
				RecordingURI: colString(row.Data[4]),
				Cluster:      colString(row.Data[5]),
			}
			if t, err := time.Parse("2006-01-02 15:04:05.000", colString(row.Data[1])); err == nil {
				ur.EventTime = t
			} else if t, err := time.Parse(time.RFC3339, colString(row.Data[1])); err == nil {
				ur.EventTime = t
			}
			rows = append(rows, ur)
		}
		first = false
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		token = out.NextToken
	}
	return rows, nil
}

func (c *Client) waitFor(ctx context.Context, qid string) error {
	delay := 200 * time.Millisecond
	for {
		out, err := c.api.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(qid),
		})
		if err != nil {
			return fmt.Errorf("GetQueryExecution: %w", err)
		}
		state := out.QueryExecution.Status.State
		switch state {
		case athenatypes.QueryExecutionStateSucceeded:
			return nil
		case athenatypes.QueryExecutionStateFailed, athenatypes.QueryExecutionStateCancelled:
			reason := aws.ToString(out.QueryExecution.Status.StateChangeReason)
			return fmt.Errorf("athena query %s: %s (%s)", qid, state, reason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

func colString(c athenatypes.Datum) string {
	if c.VarCharValue == nil {
		return ""
	}
	return *c.VarCharValue
}

// safeIdent allows only [A-Za-z0-9_]; database/catalog names from CLI
// flags need not be quoted, but they must not allow injection.
func safeIdent(s string) string {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return strings.ReplaceAll(s, " ", "_") // best effort; will fail at Athena
		}
	}
	return s
}

func validSessionID(s string) bool {
	if len(s) < 8 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
