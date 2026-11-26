-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0010: the mark of a KG that is being made (ADR-0126).
--
-- A KG is made in several transactions: its storage schema with its record (and its row of the list), then its query schemas one
-- by one. The first of them writes this mark and the last one removes it; from the first to the last the KG is closed to reads,
-- writes and ingestion and left out of the list, as one that is being deleted (kg_deleting, 0009) is. A making that stopped (its
-- server died) is deleted, not ended, once its term is over: expires_at is moved on by every transaction of the making, so a KG that
-- a live server is making is never taken for one that stopped. The mark is keyed by the id of the KG, like kg_schema_state, and
-- refers to no table: a storage schema made without a row of the list has a mark too. It goes in the transaction that deletes the
-- record. This is an expand step: it only adds, and a KG without a row is a KG that is not being made.
CREATE TABLE kg_making (
    kg_id      text COLLATE "C" NOT NULL PRIMARY KEY,
    expires_at timestamptz NOT NULL
);
