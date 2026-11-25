-- praxium:lock_timeout=3s
-- praxium:statement_timeout=60s
--
-- 0008: the token of the installation, from which the names of the roles of the query schema are made (ADR-0113).
--
-- A KG has a role for each sensitivity grade that its queries run as (pq_<token>_<kg_no>_<grade>). A role belongs to the
-- cluster and not to the database, and the number of a KG starts at 1 in every database, so two databases of one cluster
-- would make the same role. The token tells the databases apart: it is made once, when this step runs, from random bytes,
-- and it does not change. A database that is copied into another one in the same cluster has the same token and needs a
-- new one (the roles of the copy are then made again under it). This is an expand step: it only adds.
CREATE TABLE install_token (
    id    smallint PRIMARY KEY CHECK (id = 1),
    token text COLLATE "C" NOT NULL CHECK (token ~ '^[0-9a-f]{12}$')
);
INSERT INTO install_token (id, token) VALUES (1, substr(replace(gen_random_uuid()::text, '-', ''), 1, 12));
