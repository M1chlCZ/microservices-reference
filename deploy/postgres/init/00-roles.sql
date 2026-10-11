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

CREATE ROLE identity_owner NOLOGIN NOINHERIT;
CREATE ROLE identity_migrator LOGIN NOINHERIT PASSWORD 'identity-migrator-dev';
CREATE ROLE identity_app LOGIN NOINHERIT PASSWORD 'identity-app-dev';
GRANT identity_owner TO identity_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE identity_db OWNER identity_owner;
REVOKE ALL ON DATABASE identity_db FROM PUBLIC;
\connect identity_db
SET ROLE identity_owner;
GRANT CONNECT ON DATABASE identity_db TO identity_migrator, identity_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO identity_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO identity_app;
RESET ROLE;

CREATE ROLE catalog_owner NOLOGIN NOINHERIT;
CREATE ROLE catalog_migrator LOGIN NOINHERIT PASSWORD 'catalog-migrator-dev';
CREATE ROLE catalog_app LOGIN NOINHERIT PASSWORD 'catalog-app-dev';
GRANT catalog_owner TO catalog_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE catalog_db OWNER catalog_owner;
REVOKE ALL ON DATABASE catalog_db FROM PUBLIC;
\connect catalog_db
SET ROLE catalog_owner;
GRANT CONNECT ON DATABASE catalog_db TO catalog_migrator, catalog_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO catalog_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO catalog_app;
RESET ROLE;

CREATE ROLE payments_owner NOLOGIN NOINHERIT;
CREATE ROLE payments_migrator LOGIN NOINHERIT PASSWORD 'payments-migrator-dev';
CREATE ROLE payments_app LOGIN NOINHERIT PASSWORD 'payments-app-dev';
GRANT payments_owner TO payments_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE payments_db OWNER payments_owner;
REVOKE ALL ON DATABASE payments_db FROM PUBLIC;
\connect payments_db
SET ROLE payments_owner;
GRANT CONNECT ON DATABASE payments_db TO payments_migrator, payments_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO payments_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO payments_app;
RESET ROLE;

CREATE ROLE shipping_owner NOLOGIN NOINHERIT;
CREATE ROLE shipping_migrator LOGIN NOINHERIT PASSWORD 'shipping-migrator-dev';
CREATE ROLE shipping_app LOGIN NOINHERIT PASSWORD 'shipping-app-dev';
GRANT shipping_owner TO shipping_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE shipping_db OWNER shipping_owner;
REVOKE ALL ON DATABASE shipping_db FROM PUBLIC;
\connect shipping_db
SET ROLE shipping_owner;
GRANT CONNECT ON DATABASE shipping_db TO shipping_migrator, shipping_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO shipping_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO shipping_app;
RESET ROLE;

CREATE ROLE invoicing_owner NOLOGIN NOINHERIT;
CREATE ROLE invoicing_migrator LOGIN NOINHERIT PASSWORD 'invoicing-migrator-dev';
CREATE ROLE invoicing_app LOGIN NOINHERIT PASSWORD 'invoicing-app-dev';
GRANT invoicing_owner TO invoicing_migrator WITH ADMIN FALSE, INHERIT FALSE, SET TRUE;
CREATE DATABASE invoicing_db OWNER invoicing_owner;
REVOKE ALL ON DATABASE invoicing_db FROM PUBLIC;
\connect invoicing_db
SET ROLE invoicing_owner;
GRANT CONNECT ON DATABASE invoicing_db TO invoicing_migrator, invoicing_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO invoicing_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO invoicing_app;
RESET ROLE;
