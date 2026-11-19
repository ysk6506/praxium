-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0001: the tables of the storage port (kg-storage.md 3, ADR-0032).
--
-- The primary key and every index of every table start with kg_id (the schema_migrations table the runner keeps
-- is the one exception: it describes the schema, not a KG). There are no sequences: the change number is the
-- counter of the head row, so the order of commits and the order of numbers are the same. Names that are not
-- ids (object_id, link_id, ids in an entry) are text in the "C" collation, so the order of a scan is the order of
-- the bytes, as the port requires. There are no foreign keys: the checks happen where the rows are written, in
-- one transaction that holds the head row.
--
-- This is an expand step: it only adds. A later step that narrows (drops or tightens something) comes one
-- version after the code stops using it, so two adjacent versions of the server can run together.

CREATE TABLE kg (
    kg_id               text COLLATE "C" NOT NULL,
    created_no          bigint      NOT NULL,
    name                text        NOT NULL,
    role                text        NOT NULL CHECK (role IN ('production', 'candidate', 'test', 'system')),
    state               text        NOT NULL CHECK (state IN ('building', 'serving')),
    base_kg_id          text        NOT NULL DEFAULT '',
    definition_set_hash bytea       NOT NULL,
    generation          integer     NOT NULL CHECK (generation >= 1),
    created_at          timestamptz NOT NULL,
    PRIMARY KEY (kg_id)
);

-- The head row: only the applier writes it, and every write of a chunk locks it first. The change number
-- (applied_seq) is a counter in this row. hash_parts and rollback hold the partial sums by type as JSON.
CREATE TABLE kg_head (
    kg_id                    text COLLATE "C" NOT NULL,
    applied_seq              bigint  NOT NULL DEFAULT 0,
    readable_through         bigint  NOT NULL DEFAULT 0,
    applied_through          bigint  NOT NULL DEFAULT 0,
    hash_sum                 bytea   NOT NULL,
    hash_rows                bigint  NOT NULL DEFAULT 0,
    hash_parts               jsonb   NOT NULL DEFAULT '[]',
    input_digest             bytea   NOT NULL,
    apply_id                 text    NOT NULL DEFAULT '',
    next_chunk_no            integer NOT NULL DEFAULT 0,
    chunk_count              integer NOT NULL DEFAULT 0,
    rollback                 jsonb,
    last_apply_id            text    NOT NULL DEFAULT '',
    applied_semantic_version integer NOT NULL DEFAULT 0,
    PRIMARY KEY (kg_id)
);

-- The lease of the one applier. attempt only goes up, also across releases; owner and until are null when the
-- lease is not held.
CREATE TABLE kg_apply_lease (
    kg_id            text COLLATE "C" NOT NULL,
    lease_owner      text,
    lease_until      timestamptz,
    attempt          bigint  NOT NULL DEFAULT 0,
    semantic_version integer NOT NULL DEFAULT 0,
    PRIMARY KEY (kg_id)
);

CREATE TABLE generation_event (
    kg_id            text COLLATE "C" NOT NULL,
    generation       integer     NOT NULL,
    restored_to_seq  bigint      NOT NULL,
    at               timestamptz NOT NULL,
    PRIMARY KEY (kg_id, generation)
);

-- Objects: live holds the open rows, hist the closed ones. A row is valid at S when seq_from <= S < seq_to.
CREATE TABLE object_live (
    kg_id        text COLLATE "C" NOT NULL,
    object_id    text COLLATE "C" NOT NULL,
    type_no      integer NOT NULL,
    name         text    NOT NULL,
    min_grade    text    NOT NULL,
    attrs        jsonb   NOT NULL,
    source_refs  jsonb   NOT NULL,
    content_hash bytea   NOT NULL,
    seq_from     bigint  NOT NULL,
    PRIMARY KEY (kg_id, object_id)
);
CREATE INDEX object_live_by_type ON object_live (kg_id, type_no, object_id);

CREATE TABLE object_hist (
    kg_id        text COLLATE "C" NOT NULL,
    object_id    text COLLATE "C" NOT NULL,
    type_no      integer NOT NULL,
    name         text    NOT NULL,
    min_grade    text    NOT NULL,
    attrs        jsonb   NOT NULL,
    source_refs  jsonb   NOT NULL,
    content_hash bytea   NOT NULL,
    seq_from     bigint  NOT NULL,
    seq_to       bigint  NOT NULL,
    PRIMARY KEY (kg_id, object_id, seq_from)
);

CREATE TABLE link_live (
    kg_id         text COLLATE "C" NOT NULL,
    link_id       text COLLATE "C" NOT NULL,
    link_type_no  integer NOT NULL,
    src_object_id text COLLATE "C" NOT NULL,
    dst_object_id text COLLATE "C" NOT NULL,
    discriminator text    NOT NULL,
    grade         text    NOT NULL,
    basis         text    NOT NULL,
    attrs         jsonb   NOT NULL,
    source_refs   jsonb   NOT NULL,
    content_hash  bytea   NOT NULL,
    seq_from      bigint  NOT NULL,
    PRIMARY KEY (kg_id, link_id)
);
CREATE INDEX link_live_by_src ON link_live (kg_id, src_object_id, link_type_no);
CREATE INDEX link_live_by_dst ON link_live (kg_id, dst_object_id, link_type_no);

CREATE TABLE link_hist (
    kg_id         text COLLATE "C" NOT NULL,
    link_id       text COLLATE "C" NOT NULL,
    link_type_no  integer NOT NULL,
    src_object_id text COLLATE "C" NOT NULL,
    dst_object_id text COLLATE "C" NOT NULL,
    discriminator text    NOT NULL,
    grade         text    NOT NULL,
    basis         text    NOT NULL,
    attrs         jsonb   NOT NULL,
    source_refs   jsonb   NOT NULL,
    content_hash  bytea   NOT NULL,
    seq_from      bigint  NOT NULL,
    seq_to        bigint  NOT NULL,
    PRIMARY KEY (kg_id, link_id, seq_from)
);
CREATE INDEX link_hist_by_src ON link_hist (kg_id, src_object_id, link_type_no);
CREATE INDEX link_hist_by_dst ON link_hist (kg_id, dst_object_id, link_type_no);

-- The one current value of a volatile attribute. No secondary index and a low fill factor, so that an update
-- can stay in the page (postgres-operations.md 3).
CREATE TABLE attr_volatile (
    kg_id     text COLLATE "C" NOT NULL,
    object_id text COLLATE "C" NOT NULL,
    attr_no   integer NOT NULL,
    value     jsonb   NOT NULL,
    grade     text    NOT NULL,
    basis     text    NOT NULL,
    seen_at   text    NOT NULL,
    ref       text    NOT NULL,
    PRIMARY KEY (kg_id, object_id, attr_no)
) WITH (fillfactor = 70);

CREATE TABLE light_change (
    kg_id      text COLLATE "C" NOT NULL,
    change_seq bigint  NOT NULL,
    object_id  text COLLATE "C" NOT NULL,
    attr_no    integer NOT NULL,
    value_mark text    NOT NULL,
    PRIMARY KEY (kg_id, change_seq, object_id, attr_no)
);

CREATE TABLE object_identifier (
    kg_id     text COLLATE "C" NOT NULL,
    object_id text COLLATE "C" NOT NULL,
    scheme    text   NOT NULL,
    value     text   NOT NULL,
    scope     text   NOT NULL,
    seq_from  bigint NOT NULL,
    seq_to    bigint,
    PRIMARY KEY (kg_id, object_id, scheme, value, scope, seq_from)
);
CREATE INDEX object_identifier_by_value ON object_identifier (kg_id, scheme, value);

CREATE TABLE identity_event (
    kg_id             text COLLATE "C" NOT NULL,
    change_seq        bigint   NOT NULL,
    ord               integer  NOT NULL,
    event_kind        text     NOT NULL,
    retired_object_id text COLLATE "C" NOT NULL,
    into_object_id    text COLLATE "C" NOT NULL,
    link_id_map       jsonb    NOT NULL,
    PRIMARY KEY (kg_id, change_seq, ord)
);

CREATE TABLE ingest_order (
    kg_id      text COLLATE "C" NOT NULL,
    ingest_seq bigint NOT NULL,
    event_kind text   NOT NULL,
    ref        text   NOT NULL,
    digest     bytea  NOT NULL,
    change_seq bigint NOT NULL,
    PRIMARY KEY (kg_id, ingest_seq)
);

CREATE TABLE change_batch (
    kg_id        text COLLATE "C" NOT NULL,
    change_seq   bigint      NOT NULL,
    kind         text        NOT NULL,
    source_id    text        NOT NULL,
    apply_id     text        NOT NULL,
    chunk_no     integer     NOT NULL,
    chunk_count  integer     NOT NULL,
    ingest_seq   bigint      NOT NULL,
    attempt      bigint      NOT NULL,
    applied_at   timestamptz NOT NULL,
    objects      integer     NOT NULL,
    links        integer     NOT NULL,
    PRIMARY KEY (kg_id, change_seq)
);

-- ord keeps the order the applier gave the entries of a chunk.
CREATE TABLE change_entry (
    kg_id       text COLLATE "C" NOT NULL,
    change_seq  bigint  NOT NULL,
    entity_kind text    NOT NULL,
    entity_id   text COLLATE "C" NOT NULL,
    change_kind text    NOT NULL,
    old_hash    bytea,
    new_hash    bytea,
    ord         integer NOT NULL,
    PRIMARY KEY (kg_id, change_seq, entity_kind, entity_id)
);

CREATE TABLE kg_version (
    kg_id               text COLLATE "C" NOT NULL,
    version_no          integer     NOT NULL,
    version_id          text COLLATE "C" NOT NULL,
    sealed_seq          bigint      NOT NULL,
    generation          integer     NOT NULL,
    prev_version_id     text        NOT NULL,
    definition_set_hash bytea       NOT NULL,
    engine_version      integer     NOT NULL,
    hash_rule_version   integer     NOT NULL,
    input_digest        bytea       NOT NULL,
    graph_hash          bytea       NOT NULL,
    row_count           bigint      NOT NULL,
    sealed_at           timestamptz NOT NULL,
    seal_kind           text        NOT NULL,
    PRIMARY KEY (kg_id, version_no),
    UNIQUE (kg_id, version_id)
);

CREATE TABLE kg_version_hash_part (
    kg_id       text COLLATE "C" NOT NULL,
    version_no  integer NOT NULL,
    entity_kind text    NOT NULL,
    type_id     text    NOT NULL,
    partial_sum bytea   NOT NULL,
    row_count   bigint  NOT NULL,
    PRIMARY KEY (kg_id, version_no, entity_kind, type_id)
);
