-- Runs once, when the postgres data volume is empty.
-- POSTGRES_DB already created fayrly_auth.
CREATE DATABASE fayrly_user OWNER fayrly;
CREATE DATABASE fayrly_event OWNER fayrly;
CREATE DATABASE fayrly_report OWNER fayrly;
