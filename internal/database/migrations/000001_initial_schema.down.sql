DROP TRIGGER IF EXISTS update_broadcasters_updated_at ON broadcasters;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS streams;
DROP TABLE IF EXISTS stream_keys;
DROP TABLE IF EXISTS broadcasters;
DROP EXTENSION IF EXISTS "uuid-ossp";
