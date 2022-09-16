/* 
initdb -D /tmp/pub_db
initdb -D ~/pub_db
vi ~/pub_db/postgresql.conf
 update port to 5433
 set wal_level = logical
pg_ctl -D ~/pub_db  start
psql -p 5433 -d postgres -f ~/git/github.com/Infoblox-CTO/atlas.notifications.mailbox/devops/dataMigrationSourceSample.sql -v end=500000
psql $pub  -f ~/git/github.com/Infoblox-CTO/atlas.notifications.mailbox/devops/dataMigrationSourceSample.sql -v end=500000

initdb -D ~/sub_db
vi ~/sub_db/postgresql.conf
 update port to 5434
 set wal_level = logical
pg_ctl -D ~/sub_db start
psql -p 5434 -d postgres -c "create database sub;"
pg_dump -s pub -p 5433 | psql -p 5434 -d sub

pg_dump -s pub  $pub | psql -p 5434 -d sub

This sample data includes:
 * Views
 * custom data types
 * functions
 * secquence
 * tables
 * Primary index
 * Secondary index
 * composite index
 * Foreign keys
 * triggers
 * materialized view
 * extension
 */
/* AWS setup
applicable to RDS version 9.5.4 and higher

db user used by  db-controller  requires to have rds_superuser and rds_replication role.
set     rds.logical_replication static parameter to 1
(reboot to take affect)
 */
\set ON_ERROR_STOP on
-- DROP DATABASE IF EXISTS pub;
-- CREATE DATABASE pub;
-- \c pub
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE EXTENSION IF NOT EXISTS "hstore";

CREATE USER appuser PASSWORD 'secret';

CREATE SEQUENCE IF NOT EXISTS tab_1_seq;

CREATE TYPE blox_text AS (
    f1 text
);

CREATE FUNCTION fn_get_val (bigint)
    RETURNS blox_text
    AS $$
    SELECT
        'data' || CAST($1 AS text)
$$
LANGUAGE SQL;

CREATE TABLE tab_1 (
    id int PRIMARY KEY,
    name blox_text
);

CREATE UNIQUE INDEX ind_1_tab_1 ON tab_1 (id);

CREATE INDEX ind_2_tab_1 ON tab_1 (name);

CREATE TABLE tab_1_audits (
    book_id int NOT NULL,
    entry_date text NOT NULL
);

CREATE OR REPLACE FUNCTION auditfunc ()
    RETURNS TRIGGER
    AS $my_table$
BEGIN
    INSERT INTO tab_1_audits (book_id, entry_date)
        VALUES (NEW.ID, CURRENT_TIMESTAMP);
    RETURN NEW;
END;
$my_table$
LANGUAGE plpgsql;

CREATE TRIGGER price_trigger
    AFTER INSERT ON tab_1
    FOR EACH ROW
    EXECUTE PROCEDURE auditfunc ();

INSERT INTO tab_1
    VALUES (generate_series(1, :end), fn_get_val (nextval('tab_1_seq')));

CREATE SEQUENCE IF NOT EXISTS tab_2_seq;

CREATE TABLE tab_2 (
    id int PRIMARY KEY,
    name blox_text,
    tab_1_ref int REFERENCES tab_1 (id)
);

CREATE INDEX ind_1_tab_2 ON tab_2 (name, tab_1_ref);

INSERT INTO tab_2
    VALUES (generate_series(1, :end), fn_get_val (nextval('tab_2_seq')), generate_series(1, :end));

CREATE OR REPLACE VIEW vw_tab_1_2 AS
SELECT
    tab_1.id AS id,
    tab_2.id AS other_id,
    tab_1.name
FROM
    tab_1,
    tab_2
WHERE
    tab_1.id = tab_2.tab_1_ref;

-- materizlized views
CREATE MATERIALIZED VIEW mat_tab_1_2 AS
SELECT
    tab_1.id AS id,
    tab_2.id AS other_id,
    tab_1.name
FROM
    tab_1,
    tab_2
WHERE
    tab_1.id = tab_2.tab_1_ref;

-- Trigger
CREATE SEQUENCE IF NOT EXISTS tab_3_seq;

CREATE TABLE tab_3 (
    id int PRIMARY KEY,
    name blox_text,
    tab_2_ref int REFERENCES tab_1 (id)
);

CREATE INDEX ind_1_tab_3 ON tab_3 (name, tab_2_ref);

INSERT INTO tab_3
    VALUES (generate_series(1, :end), fn_get_val (nextval('tab_3_seq')), generate_series(1, :end));

CREATE TABLE tab_4 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_4
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_5 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_5
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_6 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_6
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_7 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_7
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_8 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_8
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_9 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_9
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE TABLE tab_10 (
    id int PRIMARY KEY,
    name varchar
);

INSERT INTO tab_10
    VALUES (generate_series(1, :end), 'data' || generate_series(1, :end));

CREATE OR REPLACE FUNCTION count_rows (schema text, tablename text)
    RETURNS integer
    AS $body$
DECLARE
    result integer;
    query varchar;
BEGIN
    query := 'SELECT count(1) FROM ' || schema || '.' || tablename;
    EXECUTE query INTO result;
    RETURN result;
END;
$body$
LANGUAGE plpgsql;

--CREATE publication mypub FOR ALL tables;
/*create subscription mysub connection 'dbname=pub host=localhost user=bjeevan port=5433' publication mypub;

select * from pg_stat_replication;

SELECT * FROM pg_publication;

select * from pg_create_logical_replication_slot('test_slot', 'test_decoding')
select * from pg_replication_slots

pg_drop_replication_slot('test_slot')
 */
