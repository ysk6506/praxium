-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0005: the tables of the old KG storage go (ADR-0094).
--
-- The KG storage stands on the storage schemas of the KGs now (0003, 0004, ADR-0089). The tables of 0001 are
-- not read or written by any code of this build. Nothing moves the old rows to the new storage: a database that
-- still holds a KG in the old tables is refused before this step runs (pgconn.CodeStorageFormatOld), and the
-- guard below stops this step too, in case a server that does not know the guard gets here. What it drops is
-- the empty shell of the old storage, and the storage format becomes 2.
--
-- This is the one narrowing step that does not wait for a version after the code stops using the tables
-- (ADR-0085 decision 15): the alpha releases are the only ones with such tables, and they cannot run beside
-- the new storage.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.kg) THEN
        RAISE EXCEPTION 'this database holds KGs in the storage format 1; there is no way to move them, load the sources again into a new database'
            USING ERRCODE = 'feature_not_supported';
    END IF;
END
$$;

DROP TABLE kg_version_hash_part;
DROP TABLE kg_version;
DROP TABLE change_entry;
DROP TABLE change_batch;
DROP TABLE ingest_order;
DROP TABLE identity_event;
DROP TABLE object_identifier;
DROP TABLE light_change;
DROP TABLE attr_volatile;
DROP TABLE link_hist;
DROP TABLE link_live;
DROP TABLE object_hist;
DROP TABLE object_live;
DROP TABLE generation_event;
DROP TABLE kg_apply_lease;
DROP TABLE kg_head;
DROP TABLE kg;

UPDATE storage_format SET version = 2 WHERE id = 1;
