-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0002: the records of the identity data: principals, token hashes and the bootstrap claim (ADR-0044).
--
-- These records belong to no KG, so unlike the tables of 0001 the key does not start with kg_id. A record is
-- text (the service writes JSON) with a version that goes up by one at every write; a write that expects a version
-- is an UPDATE that names it, and a write that expects the record not to exist is an INSERT that does nothing when
-- it does, so the first of two writers wins without a lock. kind and id are text in the "C" collation, so the order
-- of a scan is the order of the bytes, as the Store port of the identity unit requires.
--
-- This is an expand step: it only adds.

CREATE TABLE identity_record (
    kind    text COLLATE "C" NOT NULL,
    id      text COLLATE "C" NOT NULL,
    version bigint NOT NULL CHECK (version >= 1),
    data    text   NOT NULL,
    PRIMARY KEY (kind, id)
);
