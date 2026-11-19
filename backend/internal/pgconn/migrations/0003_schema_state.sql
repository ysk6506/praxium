-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0003: the shared tables of the schema executor (ADR-0087, ADR-0085 decision 6).
--
-- Each KG has a storage schema of its own, s_<kg_no>, that the executor of the store makes from the model; the
-- schema is not made here, because its tables depend on the model. What is shared is what the executor needs
-- before it has a schema to look at: the function that reads a time out of a document, the sequence that gives a
-- KG its number, one row per KG that says what the schema was made from, and the number of the storage format.
-- The tables of 0001 are not touched; a later step replaces them.
--
-- This is an expand step: it only adds.

-- The text of a time (UTC, ending in Z) as a timestamptz. A text-to-time cast is only STABLE, so an index could
-- not use it; this function is marked IMMUTABLE on the promise that the text names its zone, which the writers
-- of a document check before they store it. The name and the argument are the ones the plan spells
-- (pgschema.TimeFunc), because an index and the query that wants it must have the same text.
CREATE FUNCTION public.praxium_ts(text) RETURNS timestamptz
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    AS $$ SELECT $1::timestamptz $$;

-- The number of a KG, from which the name of its schema is made (s_<kg_no>). It comes from a sequence and is
-- never given again: a KG that is deleted leaves its number unused for good, so a name in a log or a backup
-- never means two KGs.
CREATE SEQUENCE kg_no_seq AS bigint START WITH 1 MINVALUE 1;

-- What the storage schema of a KG was made from. The manifest is the summary the plan keeps (pgschema.Manifest);
-- the three hashes are copied out of it so that a server compares its model with a row without reading the
-- manifest. state is where the schema has got to: ok; index_pending (an index of a change is not built yet, or
-- the run that was building it stopped); building (a run is building indexes now, since online_since);
-- stats_pending (the statistics objects are still to be made). The row is written last, in the transaction that
-- made the schema, so a schema without a row does not exist.
CREATE TABLE kg_schema_state (
    kg_id             text COLLATE "C" NOT NULL,
    kg_no             bigint      NOT NULL CHECK (kg_no >= 1),
    generator_version integer     NOT NULL,
    registry_hash     text        NOT NULL,
    numbering_hash    text        NOT NULL,
    search_hash       text        NOT NULL,
    manifest          jsonb       NOT NULL,
    state             text        NOT NULL CHECK (state IN ('ok', 'index_pending', 'building', 'stats_pending')),
    online_since      timestamptz,
    applied_at        timestamptz NOT NULL,
    PRIMARY KEY (kg_id),
    UNIQUE (kg_no)
);

-- The number of the storage format, one row. A server that finds a number larger than the one it knows does not
-- start: a later change that breaks the shape of the tables raises it, so an older executable is kept away from
-- tables it would misread.
CREATE TABLE storage_format (
    id      smallint PRIMARY KEY CHECK (id = 1),
    version integer  NOT NULL CHECK (version >= 1)
);
INSERT INTO storage_format (id, version) VALUES (1, 1);
