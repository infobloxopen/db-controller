\set ON_ERROR_STOP on
-- DROP DATABASE IF EXISTS pub;
-- CREATE DATABASE pub;
-- \c pub
-- simulate AWS rds_superuser role
CREATE ROLE rds_superuser WITH INHERIT LOGIN;

DO $$
BEGIN
    IF NOT EXISTS(
        SELECT
            *
        FROM
            pg_user
        WHERE
            usename = 'appuser') THEN
    CREATE ROLE appuser WITH INHERIT NOLOGIN;
END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS(
        SELECT
            *
        FROM
            pg_user
        WHERE
            usename = 'appuser_b') THEN
    CREATE USER appuser_b PASSWORD 'secret' IN ROLE appuser;
END IF;
END
$$;

