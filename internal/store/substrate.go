package store

// Substrate label values stamped onto every `sessions` row. The
// classifier uses these to split queries by data source.
//
// New substrates should add a constant here, a dedicated upsert path
// (see UpsertGCPSession for the pattern), and an entry in
// migrate.go's backfill if the substrate predates this column on
// any deployed databases.
const (
	// SubstrateTeleportRecording marks rows produced by the
	// Teleport-side flow (teleport-analyze).
	SubstrateTeleportRecording = "teleport-recording"

	// SubstrateGCPCloudAudit marks rows produced by shellscope-gcp.
	SubstrateGCPCloudAudit = "gcp-cloud-audit"
)
