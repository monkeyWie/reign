CREATE TABLE devices(id TEXT PRIMARY KEY,connection_id TEXT NOT NULL UNIQUE,name TEXT NOT NULL,public_key TEXT NOT NULL,token_hash TEXT NOT NULL UNIQUE,host INTEGER NOT NULL,seen INTEGER NOT NULL);
CREATE TABLE device_grants(host TEXT NOT NULL,client TEXT NOT NULL,PRIMARY KEY(host,client));
CREATE TABLE pairings(id TEXT PRIMARY KEY,host TEXT NOT NULL,client TEXT NOT NULL,name TEXT NOT NULL,public_key TEXT NOT NULL,envelope TEXT NOT NULL,reply TEXT,state TEXT NOT NULL,expires INTEGER NOT NULL);
CREATE INDEX pairings_host ON pairings(host,state);
CREATE INDEX pairings_expiry ON pairings(expires);
CREATE TABLE metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
