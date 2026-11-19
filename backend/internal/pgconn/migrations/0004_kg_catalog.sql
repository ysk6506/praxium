-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0004: the list of KGs and the numbers of the new KG storage (ADR-0089).
--
-- A KG stands on its own in its storage schema (s_<kg_no>), the control rows included: its head, its lease, its
-- generations and its sealed versions are there, and so is every table that grows with its rows. What is shared
-- is the list of KGs (what a name, a role and a base KG say, and where the KG lives) and the two numbers that no
-- KG may give twice: the number of an object and the number of a link. This is an expand step: it only adds. The
-- tables of 0001 are not touched; the change that puts the new storage in place of the old one drops them.

-- One row per KG. The state of a KG (building, serving) and its generation change with the applies and the
-- restores, which write them in the same transaction as the rows of the KG, so they are in the head of the
-- KG's own schema and not here. kg_no is the number of the schema; it comes from kg_no_seq and is never given
-- again. cluster is where the KG lives: empty means the database this row is in.
CREATE TABLE kg_catalog (
    kg_id               text COLLATE "C" NOT NULL,
    kg_no               bigint      NOT NULL CHECK (kg_no >= 1),
    name                text        NOT NULL,
    role                text        NOT NULL CHECK (role IN ('production', 'candidate', 'test', 'system')),
    base_kg_id          text        NOT NULL DEFAULT '',
    definition_set_hash bytea       NOT NULL,
    cluster             text        NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL,
    PRIMARY KEY (kg_id),
    UNIQUE (kg_no)
);
CREATE UNIQUE INDEX kg_catalog_name ON kg_catalog (name);

-- The numbers of objects and links. A number is given once in the whole database and is not given again: a
-- number that is taken and not used (an apply that was abandoned, a transaction that failed) is a gap. The
-- store asks for numbers through the two functions only, so that a later change can give them from a sequence
-- of each KG or from a range without touching a caller.
CREATE SEQUENCE obj_no_seq AS bigint START WITH 1 MINVALUE 1 CACHE 1000;
CREATE SEQUENCE link_no_seq AS bigint START WITH 1 MINVALUE 1 CACHE 1000;

CREATE FUNCTION public.praxium_next_obj_nos(n integer) RETURNS SETOF bigint
    LANGUAGE sql VOLATILE
    AS $$ SELECT nextval('public.obj_no_seq') FROM generate_series(1, n) $$;

CREATE FUNCTION public.praxium_next_link_nos(n integer) RETURNS SETOF bigint
    LANGUAGE sql VOLATILE
    AS $$ SELECT nextval('public.link_no_seq') FROM generate_series(1, n) $$;
