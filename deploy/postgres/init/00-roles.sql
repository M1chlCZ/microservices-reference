-- Development roles and databases for the reference stack. The entrypoint of
-- the compose Postgres image runs this file on a fresh data volume only.
-- The passwords are local development values.

CREATE ROLE orders_owner NOLOGIN NOINHERIT;
CREATE ROLE orders_migrator LOGIN NOINHERIT PASSWORD 'orders-migrator-dev';
CREATE ROLE orders_app LOGIN NOINHERIT PASSWORD 'orders-app-dev';
GRANT orders_owner TO orders_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE orders_db OWNER orders_owner;
REVOKE ALL ON DATABASE orders_db FROM PUBLIC;
\connect orders_db
SET ROLE orders_owner;
GRANT CONNECT ON DATABASE orders_db TO orders_migrator, orders_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO orders_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO orders_app;
RESET ROLE;

CREATE ROLE inventory_owner NOLOGIN NOINHERIT;
CREATE ROLE inventory_migrator LOGIN NOINHERIT PASSWORD 'inventory-migrator-dev';
CREATE ROLE inventory_app LOGIN NOINHERIT PASSWORD 'inventory-app-dev';
GRANT inventory_owner TO inventory_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE inventory_db OWNER inventory_owner;
REVOKE ALL ON DATABASE inventory_db FROM PUBLIC;
\connect inventory_db
SET ROLE inventory_owner;
GRANT CONNECT ON DATABASE inventory_db TO inventory_migrator, inventory_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO inventory_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO inventory_app;
RESET ROLE;

CREATE ROLE notifications_owner NOLOGIN NOINHERIT;
CREATE ROLE notifications_migrator LOGIN NOINHERIT PASSWORD 'notifications-migrator-dev';
CREATE ROLE notifications_app LOGIN NOINHERIT PASSWORD 'notifications-app-dev';
GRANT notifications_owner TO notifications_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE notifications_db OWNER notifications_owner;
REVOKE ALL ON DATABASE notifications_db FROM PUBLIC;
\connect notifications_db
SET ROLE notifications_owner;
GRANT CONNECT ON DATABASE notifications_db TO notifications_migrator, notifications_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO notifications_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO notifications_app;
RESET ROLE;
