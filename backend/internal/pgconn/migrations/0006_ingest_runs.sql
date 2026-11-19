-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0006: the record of the runs of the ingestion at scale and the state of the sources (ADR-0086, ADR-0095).
--
-- A run is one source's ingestion, from the receiving of its pages to the cleaning after the apply. Its row says
-- where it has got to, so that any server can take it over from there; it is in the shared schema because a source
-- and its runs are listed across KGs. What grows with the rows of a KG (the raw records, the staged rows) is in the
-- storage schema of the KG (pgschema.TableRawRecord and the others). This is an expand step: it only adds.

-- The numbers: of a run (the raw record keeps the number of the run that opened it, so it only goes up), of a
-- source, and of a dataset. The numbers of sources and datasets are the keys of the partitions and of the staged
-- rows in the storage schemas; they are given once for the whole database.
CREATE SEQUENCE sync_run_no_seq AS bigint START WITH 1 MINVALUE 1;
CREATE SEQUENCE source_no_seq AS integer START WITH 1 MINVALUE 1;
CREATE SEQUENCE dataset_no_seq AS integer START WITH 1 MINVALUE 1;

-- A source put to a KG. full_receive is the mark that the next run receives every row of the source with its
-- fields, and full_compute that the next run computes the whole KG (a restore of the KG, a changed mapping, a run
-- that was discarded: ingestion-at-scale.md 4). A source is new until its first run, so both start set.
CREATE TABLE source_state (
    src_no                integer     NOT NULL DEFAULT nextval('source_no_seq'),
    kg_id                 text COLLATE "C" NOT NULL,
    source_id             text COLLATE "C" NOT NULL,
    full_receive          boolean     NOT NULL DEFAULT true,
    full_compute          boolean     NOT NULL DEFAULT true,
    applied_definition_set bytea,
    created_at             timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (src_no),
    UNIQUE (kg_id, source_id)
);

-- A dataset of a source. capture_hash and decl_rev are those of the last complete capture (the place of the
-- shortcut for a capture that has the same bytes); cursor is the place of the incremental and the stream.
CREATE TABLE source_dataset (
    dataset_no   integer NOT NULL DEFAULT nextval('dataset_no_seq'),
    src_no       integer NOT NULL REFERENCES source_state (src_no) ON DELETE CASCADE,
    dataset_id   text COLLATE "C" NOT NULL,
    capture_hash bytea,
    decl_rev     integer     NOT NULL DEFAULT 0,
    cursor       text,
    captured_at  timestamptz,
    PRIMARY KEY (dataset_no),
    UNIQUE (src_no, dataset_id)
);

-- The runs. stage is the work and then the end of a run. The place to go on from is in the columns of the stage
-- (the dataset, the page and the token of the receiving; the job and the part of the computing, the volatile
-- values and the cleaning); the place of the apply is in the head of the KG, which an apply writes in its own
-- transaction. owner, attempt and lease_until say which server does the run: the lease is judged by the clock of
-- the database, and a write of a server that woke up late is stopped by the attempt it names. end_class says how a
-- run that failed failed: a setting the person mends (config), trouble that passes and may be tried again
-- (transient), or a fault of the product.
CREATE TABLE sync_run (
    run_no              bigint      NOT NULL DEFAULT nextval('sync_run_no_seq'),
    kg_id               text COLLATE "C" NOT NULL,
    src_no              integer     NOT NULL REFERENCES source_state (src_no) ON DELETE CASCADE,
    kind                text        NOT NULL DEFAULT 'full' CHECK (kind IN ('full', 'incremental', 'stream')),
    trigger_kind        text        NOT NULL DEFAULT 'manual',
    apply_mode          text        NOT NULL DEFAULT 'automatic' CHECK (apply_mode IN ('automatic', 'after_confirmation')),
    stage               text        NOT NULL CHECK (stage IN ('receiving', 'computing', 'awaiting_confirmation', 'applying', 'volatile', 'settling', 'succeeded', 'failed', 'canceled', 'discarded')),
    recv_dataset_no     integer,
    recv_page           integer     NOT NULL DEFAULT 0,
    recv_token          text,
    calc_job            smallint    NOT NULL DEFAULT 0,
    calc_part           integer     NOT NULL DEFAULT 0,
    volatile_part       integer     NOT NULL DEFAULT 0,
    settle_job          smallint    NOT NULL DEFAULT 0,
    settle_part         integer     NOT NULL DEFAULT 0,
    counts              jsonb       NOT NULL DEFAULT '{}',
    apply_id            text        NOT NULL DEFAULT '',
    definition_set_hash bytea,
    change_fingerprint  bytea,
    owner               text,
    attempt             bigint      NOT NULL DEFAULT 0,
    lease_until         timestamptz,
    started_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    ended_at            timestamptz,
    end_class           text        CHECK (end_class IN ('config', 'transient', 'product')),
    end_code            text,
    end_reason          text,
    PRIMARY KEY (run_no),
    CHECK ((stage = 'failed') = (end_class IS NOT NULL)),
    CHECK ((stage IN ('succeeded', 'failed', 'canceled', 'discarded')) = (ended_at IS NOT NULL))
);

-- The gate: a source has one run that is not over, and a KG has one run that computes, applies, writes the
-- volatile values or cleans. A run that waits for a confirmation lets the gate of the KG go (it computes again
-- when it is confirmed), so the second index does not hold it.
CREATE UNIQUE INDEX sync_run_one_open_per_source ON sync_run (src_no)
    WHERE stage NOT IN ('succeeded', 'failed', 'canceled', 'discarded');
CREATE UNIQUE INDEX sync_run_one_working_per_kg ON sync_run (kg_id)
    WHERE stage IN ('computing', 'applying', 'volatile', 'settling');
-- The list of the runs of a KG and of a source, the newest first.
CREATE INDEX sync_run_kg ON sync_run (kg_id, run_no DESC);
CREATE INDEX sync_run_source ON sync_run (src_no, run_no DESC);
