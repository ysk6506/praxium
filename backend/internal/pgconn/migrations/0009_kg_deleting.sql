-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0009: the mark of a KG that is being deleted (ADR-0125).
--
-- A KG is deleted in several transactions: its sources one by one, then the tables of the computation, then the rest of
-- its storage schema with its row of the list. The first of them writes this mark, and from then on the KG is closed to
-- reads, writes and ingestion and left out of the list; a server that finds the mark ends the delete. The mark goes with
-- the row of the list in the last transaction. This is an expand step: it only adds, and a KG without a row is a KG that
-- is not being deleted.
CREATE TABLE kg_deleting (
    kg_id      text COLLATE "C" NOT NULL PRIMARY KEY REFERENCES kg_catalog (kg_id) ON DELETE CASCADE,
    started_at timestamptz NOT NULL
);
