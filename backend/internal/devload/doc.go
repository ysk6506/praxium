// Package devload reads the ground truth of the world generator and applies it to a KG through the
// applier, as change batches of bounded size (ADR-0027).
//
// It is a developer convenience and a test path. It is NOT the ingestion pipeline: no source, no
// connector, no reconciliation and no ordering of real changes is in it. The truth is taken as one
// complete and consistent world and written into a KG that is empty. What it gives is a KG with the
// shape of a real estate to read through the queries, the API and the command line while the pipeline
// does not exist yet.
//
// The unit may import the kernel, the model, the graph unit and the generator (for the reader of the
// truth file). It does not import an adapter: the store comes in as the storage port.
package devload
